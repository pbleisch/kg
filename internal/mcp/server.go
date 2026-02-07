// Package mcp implements a minimal MCP (Model Context Protocol) server
// over JSON-RPC 2.0 on stdio, exposing kg query and command operations as tools.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/store"
)

// JSON-RPC 2.0 message types.

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any         `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP protocol types.

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
}

type capabilities struct {
	Tools *toolsCap `json:"tools,omitempty"`
}

type toolsCap struct{}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema jsonSchema `json:"inputSchema"`
}

type jsonSchema struct {
	Type       string                `json:"type"`
	Properties map[string]schemaProp `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
}

type schemaProp struct {
	Type        string     `json:"type"`
	Description string     `json:"description,omitempty"`
	Items       *itemsDef  `json:"items,omitempty"`
	Properties  map[string]schemaProp `json:"properties,omitempty"`
}

type itemsDef struct {
	Type       string                `json:"type,omitempty"`
	Properties map[string]schemaProp `json:"properties,omitempty"`
}

type toolsListResult struct {
	Tools []toolDef `json:"tools"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Server is a minimal MCP server that reads/writes JSON-RPC 2.0 on stdio.
type Server struct {
	kgDir string
	tools []toolDef
}

// NewServer creates a new MCP server targeting the given kg directory.
func NewServer(kgDir string) *Server {
	return &Server{
		kgDir: kgDir,
		tools: buildToolDefs(),
	}
}

// Run reads JSON-RPC requests from r and writes responses to w until EOF.
func (s *Server) Run(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// MCP uses newline-delimited JSON.
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := response{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			}
			writeResponse(w, resp)
			continue
		}

		// Notifications (no ID) don't get responses.
		if req.ID == nil {
			continue
		}

		resp := s.handle(req)
		writeResponse(w, resp)
	}
	return scanner.Err()
}

func (s *Server) handle(req request) response {
	switch req.Method {
	case "initialize":
		return response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: initializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities:    capabilities{Tools: &toolsCap{}},
				ServerInfo:      serverInfo{Name: "kg", Version: "0.1.0"},
			},
		}
	case "tools/list":
		return response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  toolsListResult{Tools: s.tools},
		}
	case "tools/call":
		return s.handleToolCall(req)
	default:
		return response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleToolCall(req request) response {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "invalid params"},
		}
	}

	result, err := s.dispatch(params.Name, params.Arguments)
	if err != nil {
		return response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolCallResult{
				Content: []toolContent{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		}
	}

	text, _ := json.Marshal(result)
	return response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: toolCallResult{
			Content: []toolContent{{Type: "text", Text: string(text)}},
		},
	}
}

func (s *Server) dispatch(name string, args json.RawMessage) (any, error) {
	switch name {
	case "kg_get":
		return s.toolGet(args)
	case "kg_find":
		return s.toolFind(args)
	case "kg_back":
		return s.toolBack(args)
	case "kg_path":
		return s.toolPath(args)
	case "kg_create":
		return s.toolCreate(args)
	case "kg_set":
		return s.toolSet(args)
	case "kg_delete":
		return s.toolDelete(args)
	case "kg_link":
		return s.toolLink(args)
	case "kg_unlink":
		return s.toolUnlink(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// Tool implementations.

func (s *Server) toolGet(args json.RawMessage) (*store.Entity, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return query.Get(s.kgDir, p.ID)
}

func (s *Server) toolFind(args json.RawMessage) ([]*store.Entity, error) {
	var p struct {
		Type    string `json:"type"`
		Filters []struct {
			Attr  string `json:"attr"`
			Op    string `json:"op"`
			Value string `json:"value"`
		} `json:"filters"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	var filters []query.Filter
	for _, f := range p.Filters {
		filters = append(filters, query.Filter{Attr: f.Attr, Op: f.Op, Value: f.Value})
	}
	return query.Find(s.kgDir, p.Type, filters)
}

func (s *Server) toolBack(args json.RawMessage) ([]*store.Entity, error) {
	var p struct {
		TargetID string `json:"target_id"`
		Relation string `json:"relation"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return query.Back(s.kgDir, p.TargetID, p.Relation)
}

func (s *Server) toolPath(args json.RawMessage) ([]string, error) {
	var p struct {
		StartID string `json:"start_id"`
		Steps   []struct {
			Relation  string `json:"relation"`
			Recursive bool   `json:"recursive"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	var steps []query.PathStep
	for _, st := range p.Steps {
		steps = append(steps, query.PathStep{Relation: st.Relation, Recursive: st.Recursive})
	}
	return query.Path(s.kgDir, p.StartID, steps)
}

func (s *Server) toolCreate(args json.RawMessage) (*store.Entity, error) {
	var p struct {
		Type       string            `json:"type"`
		LocalID    string            `json:"local_id"`
		Attributes map[string]string `json:"attributes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return command.Create(s.kgDir, p.Type, p.LocalID, p.Attributes)
}

func (s *Server) toolSet(args json.RawMessage) (*store.Entity, error) {
	var p struct {
		ID         string            `json:"id"`
		Attributes map[string]string `json:"attributes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return command.Set(s.kgDir, p.ID, p.Attributes)
}

type deleteResult struct {
	Deleted bool `json:"deleted"`
}

func (s *Server) toolDelete(args json.RawMessage) (*deleteResult, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if err := command.Delete(s.kgDir, p.ID); err != nil {
		return nil, err
	}
	return &deleteResult{Deleted: true}, nil
}

type linkResult struct {
	OK bool `json:"ok"`
}

func (s *Server) toolLink(args json.RawMessage) (*linkResult, error) {
	var p struct {
		SourceID string `json:"source_id"`
		Relation string `json:"relation"`
		TargetID string `json:"target_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if err := command.Link(s.kgDir, p.SourceID, p.Relation, p.TargetID); err != nil {
		return nil, err
	}
	return &linkResult{OK: true}, nil
}

func (s *Server) toolUnlink(args json.RawMessage) (*linkResult, error) {
	var p struct {
		SourceID string `json:"source_id"`
		Relation string `json:"relation"`
		TargetID string `json:"target_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if err := command.Unlink(s.kgDir, p.SourceID, p.Relation, p.TargetID); err != nil {
		return nil, err
	}
	return &linkResult{OK: true}, nil
}

// writeResponse marshals and writes a JSON-RPC response followed by a newline.
func writeResponse(w io.Writer, resp response) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	w.Write(data)
}

// buildToolDefs returns the MCP tool definitions for all exposed kg operations.
func buildToolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "kg_get",
			Description: "Get a single entity by ID (e.g. \"person:alice\")",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id": {Type: "string", Description: "Entity ID in type:localID format"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "kg_find",
			Description: "Find entities by type with optional attribute filters",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"type": {Type: "string", Description: "Entity type name (e.g. \"Person\")"},
					"filters": {
						Type:        "array",
						Description: "Optional attribute filters",
						Items: &itemsDef{
							Type: "object",
							Properties: map[string]schemaProp{
								"attr":  {Type: "string", Description: "Attribute name"},
								"op":    {Type: "string", Description: "Operator: =, !=, >, <, >=, <=, ~"},
								"value": {Type: "string", Description: "Value to compare"},
							},
						},
					},
				},
				Required: []string{"type"},
			},
		},
		{
			Name:        "kg_back",
			Description: "Find entities that point to a target entity via a relation (reverse lookup)",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"target_id": {Type: "string", Description: "Target entity ID"},
					"relation":  {Type: "string", Description: "Relation name to follow in reverse"},
				},
				Required: []string{"target_id", "relation"},
			},
		},
		{
			Name:        "kg_path",
			Description: "Traverse the graph from a start entity following a sequence of relation steps",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"start_id": {Type: "string", Description: "Starting entity ID"},
					"steps": {
						Type:        "array",
						Description: "Relation steps to traverse",
						Items: &itemsDef{
							Type: "object",
							Properties: map[string]schemaProp{
								"relation":  {Type: "string", Description: "Relation name"},
								"recursive": {Type: "boolean", Description: "Follow transitively if true"},
							},
						},
					},
				},
				Required: []string{"start_id", "steps"},
			},
		},
		{
			Name:        "kg_create",
			Description: "Create a new entity with a type, local ID, and attributes",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"type":       {Type: "string", Description: "Entity type name"},
					"local_id":   {Type: "string", Description: "Local identifier (combined with type to form entity ID)"},
					"attributes": {Type: "object", Description: "Key-value attributes"},
				},
				Required: []string{"type", "local_id"},
			},
		},
		{
			Name:        "kg_set",
			Description: "Update attributes on an existing entity (merge semantics)",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":         {Type: "string", Description: "Entity ID"},
					"attributes": {Type: "object", Description: "Key-value attributes to set/update"},
				},
				Required: []string{"id", "attributes"},
			},
		},
		{
			Name:        "kg_delete",
			Description: "Delete an entity by ID",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id": {Type: "string", Description: "Entity ID to delete"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "kg_link",
			Description: "Add a relation link from source entity to target entity",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"source_id": {Type: "string", Description: "Source entity ID"},
					"relation":  {Type: "string", Description: "Relation name"},
					"target_id": {Type: "string", Description: "Target entity ID"},
				},
				Required: []string{"source_id", "relation", "target_id"},
			},
		},
		{
			Name:        "kg_unlink",
			Description: "Remove a relation link from source entity to target entity",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"source_id": {Type: "string", Description: "Source entity ID"},
					"relation":  {Type: "string", Description: "Relation name"},
					"target_id": {Type: "string", Description: "Target entity ID"},
				},
				Required: []string{"source_id", "relation", "target_id"},
			},
		},
	}
}
