// Package mcp implements a minimal MCP (Model Context Protocol) server
// over JSON-RPC 2.0 on stdio, exposing kg query and command operations as tools.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/pbleisch/kg/pkg/batch"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/schema"
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
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
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
	Type        string                `json:"type"`
	Description string                `json:"description,omitempty"`
	Items       *itemsDef             `json:"items,omitempty"`
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
	case "kg_unset":
		return s.toolUnset(args)
	case "kg_delete":
		return s.toolDelete(args)
	case "kg_link":
		return s.toolLink(args)
	case "kg_unlink":
		return s.toolUnlink(args)
	case "kg_types":
		return s.toolTypes(args)
	case "kg_describe":
		return s.toolDescribe(args)
	case "kg_stats":
		return s.toolStats(args)
	case "kg_validate":
		return s.toolValidate(args)
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
	return batch.CreateEntity(s.kgDir, p.Type, p.LocalID, p.Attributes, false)
}

func (s *Server) toolSet(args json.RawMessage) (*store.Entity, error) {
	var p struct {
		ID         string            `json:"id"`
		Attributes map[string]string `json:"attributes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return batch.SetAttrs(s.kgDir, p.ID, p.Attributes)
}

func (s *Server) toolUnset(args json.RawMessage) (*store.Entity, error) {
	var p struct {
		ID         string   `json:"id"`
		Attributes []string `json:"attributes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return batch.UnsetAttrs(s.kgDir, p.ID, p.Attributes)
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
	if err := batch.DeleteRef(s.kgDir, p.ID); err != nil {
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
	if err := batch.LinkRef(s.kgDir, p.SourceID, p.Relation, p.TargetID); err != nil {
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
	if err := batch.UnlinkRef(s.kgDir, p.SourceID, p.Relation, p.TargetID); err != nil {
		return nil, err
	}
	return &linkResult{OK: true}, nil
}

// Schema introspection tools.

func (s *Server) toolTypes(args json.RawMessage) ([]string, error) {
	schemas, err := schema.LoadAllSchemas(s.kgDir)
	if err != nil {
		return nil, fmt.Errorf("loading schemas: %w", err)
	}
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

type schemaResult struct {
	Type        string       `json:"type"`
	Description string       `json:"description"`
	Attributes  []attrResult `json:"attributes"`
	Relations   []relResult  `json:"relations"`
}

type attrResult struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type relResult struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Cardinality string `json:"cardinality"`
}

func (s *Server) toolDescribe(args json.RawMessage) (*schemaResult, error) {
	var p struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	sc, err := schema.LoadSchema(s.kgDir, p.Type)
	if err != nil {
		return nil, err
	}

	attrs := make([]attrResult, 0, len(sc.Attributes))
	for name, a := range sc.Attributes {
		attrs = append(attrs, attrResult{Name: name, Type: a.Type, Required: a.Required})
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })

	rels := make([]relResult, 0, len(sc.Relations))
	for name, r := range sc.Relations {
		rels = append(rels, relResult{Name: name, Target: r.Target, Cardinality: r.Cardinality})
	}
	sort.Slice(rels, func(i, j int) bool { return rels[i].Name < rels[j].Name })

	return &schemaResult{
		Type:        sc.Type,
		Description: sc.Description,
		Attributes:  attrs,
		Relations:   rels,
	}, nil
}

func (s *Server) toolStats(args json.RawMessage) (*query.GraphStats, error) {
	return query.Stats(s.kgDir)
}

// validateResult reports both halves of P3, separately.  An agent has to be
// able to tell "this graph does not load" from "this graph is incomplete", and
// one error list cannot say both.
type validateResult struct {
	// Valid is whether the graph loads.  Warnings do not affect it.
	Valid         bool          `json:"valid"`
	TotalFiles    int           `json:"total_files"`
	TotalEntities int           `json:"total_entities"`
	ErrorCount    int           `json:"error_count"`
	WarningCount  int           `json:"warning_count"`
	Errors        []validateErr `json:"errors"`
	Warnings      []validateErr `json:"warnings"`
}

type validateErr struct {
	Code     string   `json:"code"`
	EntityID string   `json:"entity_id,omitempty"`
	Files    []string `json:"files,omitempty"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
}

func (s *Server) toolValidate(args json.RawMessage) (*validateResult, error) {
	rep, err := query.Validate(s.kgDir)
	if err != nil {
		return nil, err
	}
	res := &validateResult{
		Valid:         rep.OK(),
		TotalFiles:    rep.Files,
		TotalEntities: rep.Entities,
		ErrorCount:    len(rep.Structural),
		WarningCount:  len(rep.Advisories),
		Errors:        make([]validateErr, 0, len(rep.Structural)),
		Warnings:      make([]validateErr, 0, len(rep.Advisories)),
	}
	for _, e := range rep.Structural {
		res.Errors = append(res.Errors, validateErr{
			Code: e.Code, EntityID: e.ID, Files: e.Files, Field: e.Field, Message: e.Message,
		})
	}
	for _, a := range rep.Advisories {
		res.Warnings = append(res.Warnings, validateErr{
			Code: a.Code, EntityID: a.ID, Files: filesOf(a.File), Field: a.Field, Message: a.Message,
		})
	}
	return res, nil
}

func filesOf(path string) []string {
	if path == "" {
		return nil
	}
	return []string{path}
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
			Description: "Assign attributes on an existing entity. `set` overwrites; `create` with upsert merges by field kind",
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
			Name:        "kg_unset",
			Description: "Remove named attributes from an entity. The only way kg removes an attribute, and the only thing that removes an item carrying no source",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":         {Type: "string", Description: "Entity ID"},
					"attributes": {Type: "array", Description: "Attribute names to remove"},
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
		{
			Name:        "kg_types",
			Description: "List all available entity types in the knowledge graph schema",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{},
			},
		},
		{
			Name:        "kg_describe",
			Description: "Describe a schema type showing its attributes, relations, and constraints",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"type": {Type: "string", Description: "Schema type name (e.g. \"Concept\", \"Game\", \"Person\")"},
				},
				Required: []string{"type"},
			},
		},
		{
			Name:        "kg_stats",
			Description: "Get graph statistics: entity counts, link counts, type breakdown, and orphan count",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{},
			},
		},
		{
			Name:        "kg_validate",
			Description: "Check the graph. Structural findings (an identity two files claim, a declared id or type that disagrees with the file's location, an illegal attribute name, a file that will not parse) mean it does not load and come back as errors; content findings (unknown attributes, schema violations, relation targets that name nothing) come back as warnings and do not.",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{},
			},
		},
	}
}
