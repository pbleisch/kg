package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/pbleisch/kg/internal/daemon"
	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent integration and onboarding tools",
	}

	agentCmd.AddCommand(
		agentStatusCmd(),
		agentSchemaCmd(),
		agentSkillCmd(),
		agentExamplesCmd(),
		agentMCPConfigCmd(),
		agentCheckCmd(),
	)

	rootCmd.AddCommand(agentCmd)
}

// ---------------------------------------------------------------------------
// agent status
// ---------------------------------------------------------------------------

func agentStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show agent-friendly status of the knowledge graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			absDir, err := filepath.Abs(cfg.kgDir)
			if err != nil {
				return err
			}

			status := agentStatus{Dir: absDir}

			// Entity counts by type.
			entities, err := store.ListEntities(cfg.kgDir)
			if err == nil {
				counts := map[string]int{}
				for _, e := range entities {
					counts[e.Type]++
				}
				status.EntityCount = len(entities)
				status.EntitiesByType = counts
			}

			// Schema types.
			schemas, err := schema.LoadAllSchemas(cfg.kgDir)
			if err == nil {
				names := make([]string, 0, len(schemas))
				for n := range schemas {
					names = append(names, n)
				}
				sort.Strings(names)
				status.SchemaTypes = names
			}

			// Index freshness.
			status.IndexStatus = indexFreshness(cfg.kgDir)
			status.Backend = store.ActiveBackend(cfg.kgDir)

			// Daemon status.
			status.DaemonStatus = daemonRunning(absDir)

			return writeOutput(cmd.OutOrStdout(), status)
		},
	}
}

type agentStatus struct {
	Dir            string         `json:"dir"`
	Backend        string         `json:"backend"`
	EntityCount    int            `json:"entity_count"`
	EntitiesByType map[string]int `json:"entities_by_type,omitempty"`
	SchemaTypes    []string       `json:"schema_types,omitempty"`
	IndexStatus    string         `json:"index_status"`
	DaemonStatus   string         `json:"daemon_status"`
}

func (s agentStatus) toTOON(w io.Writer) {
	fmt.Fprintf(w, "dir: %s\n", s.Dir)
	fmt.Fprintf(w, "backend: %s\n", s.Backend)
	fmt.Fprintf(w, "entity_count: %d\n", s.EntityCount)
	if len(s.EntitiesByType) > 0 {
		types := make([]string, 0, len(s.EntitiesByType))
		for t := range s.EntitiesByType {
			types = append(types, t)
		}
		sort.Strings(types)
		parts := make([]string, len(types))
		for i, t := range types {
			parts[i] = fmt.Sprintf("%s=%d", t, s.EntitiesByType[t])
		}
		fmt.Fprintf(w, "entities_by_type{%s}: %s\n", strings.Join(types, ","), strings.Join(parts, ","))
	}
	if len(s.SchemaTypes) > 0 {
		fmt.Fprintf(w, "schema_types[%d]: %s\n", len(s.SchemaTypes), strings.Join(s.SchemaTypes, ","))
	}
	fmt.Fprintf(w, "index_status: %s\n", s.IndexStatus)
	fmt.Fprintf(w, "daemon_status: %s\n", s.DaemonStatus)
}

// ---------------------------------------------------------------------------
// agent schema
// ---------------------------------------------------------------------------

func agentSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Dump all schemas in agent-readable format",
		RunE: func(cmd *cobra.Command, args []string) error {
			schemas, err := schema.LoadAllSchemas(cfg.kgDir)
			if err != nil {
				return fmt.Errorf("loading schemas: %w", err)
			}

			dump := make([]schemaDump, 0, len(schemas))
			names := make([]string, 0, len(schemas))
			for n := range schemas {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				s := schemas[n]
				d := schemaDump{
					Type:        s.Type,
					Description: s.Description,
				}
				attrNames := sortedMapKeys(s.Attributes)
				for _, an := range attrNames {
					a := s.Attributes[an]
					d.Attributes = append(d.Attributes, attrDump{
						Name:     an,
						Type:     a.Type,
						Required: a.Required,
					})
				}
				relNames := sortedMapKeys(s.Relations)
				for _, rn := range relNames {
					r := s.Relations[rn]
					d.Relations = append(d.Relations, relDump{
						Name:        rn,
						Target:      r.Target,
						Cardinality: r.Cardinality,
					})
				}
				dump = append(dump, d)
			}

			return writeOutput(cmd.OutOrStdout(), schemaOutput{Schemas: dump})
		},
	}
}

type attrDump struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type relDump struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Cardinality string `json:"cardinality"`
}

type schemaDump struct {
	Type        string     `json:"type"`
	Description string     `json:"description,omitempty"`
	Attributes  []attrDump `json:"attributes,omitempty"`
	Relations   []relDump  `json:"relations,omitempty"`
}

type schemaOutput struct {
	Schemas []schemaDump `json:"schemas"`
}

func (o schemaOutput) toTOON(w io.Writer) {
	fmt.Fprintf(w, "schemas[%d]:\n", len(o.Schemas))
	for _, s := range o.Schemas {
		fmt.Fprintf(w, "  type: %s\n", s.Type)
		if s.Description != "" {
			fmt.Fprintf(w, "  description: %s\n", s.Description)
		}
		if len(s.Attributes) > 0 {
			fmt.Fprintf(w, "  attributes[%d]:\n", len(s.Attributes))
			for _, a := range s.Attributes {
				req := ""
				if a.Required {
					req = " (required)"
				}
				fmt.Fprintf(w, "    %s: %s%s\n", a.Name, a.Type, req)
			}
		}
		if len(s.Relations) > 0 {
			fmt.Fprintf(w, "  relations[%d]:\n", len(s.Relations))
			for _, r := range s.Relations {
				fmt.Fprintf(w, "    %s: -> %s [%s]\n", r.Name, r.Target, r.Cardinality)
			}
		}
		fmt.Fprintln(w)
	}
}

// ---------------------------------------------------------------------------
// agent skill
// ---------------------------------------------------------------------------

func agentSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Generate a SKILL.md for agent integration",
		RunE: func(cmd *cobra.Command, args []string) error {
			githubFlag, _ := cmd.Flags().GetBool("github")
			claudeFlag, _ := cmd.Flags().GetBool("claude")

			schemas, _ := schema.LoadAllSchemas(cfg.kgDir)
			typeNames := make([]string, 0, len(schemas))
			for n := range schemas {
				typeNames = append(typeNames, n)
			}
			sort.Strings(typeNames)

			data := skillData{
				KGDir:   cfg.kgDir,
				Types:   typeNames,
				Schemas: schemas,
			}

			var buf strings.Builder
			tmpl := template.Must(template.New("skill").Parse(skillTemplate))
			if err := tmpl.Execute(&buf, data); err != nil {
				return fmt.Errorf("generating skill: %w", err)
			}

			content := buf.String()

			switch {
			case githubFlag:
				path := filepath.Join(".github", "skills", "kg", "SKILL.md")
				return writeSkillFile(path, content)
			case claudeFlag:
				path := filepath.Join(".claude", "skills", "kg.md")
				return writeSkillFile(path, content)
			default:
				fmt.Fprint(cmd.OutOrStdout(), content)
				return nil
			}
		},
	}
	cmd.Flags().Bool("github", false, "write to .github/skills/kg/SKILL.md")
	cmd.Flags().Bool("claude", false, "write to .claude/skills/kg.md")
	return cmd
}

type skillData struct {
	KGDir   string
	Types   []string
	Schemas map[string]*schema.Schema
}

func writeSkillFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	return nil
}

const skillTemplate = `---
name: kg
description: >
  Use the kg CLI to query and mutate the knowledge graph.
  Available commands: create, get, find, set, delete, link, unlink, path, back, describe, types, validate.
---

# Using kg (Knowledge Graph CLI)

## Quick Reference

` + "```" + `bash
kg types --dir={{.KGDir}}                     # List schema types
kg describe <type> --dir={{.KGDir}}           # Show type schema
kg create <type> <id> [attrs...] --dir={{.KGDir}}  # Create entity
kg get <type:id> --dir={{.KGDir}}             # Get entity by ID
kg find <type> [filters...] --dir={{.KGDir}}  # Find entities
kg set <type:id> key=value --dir={{.KGDir}}   # Set attributes
kg delete <type:id> --dir={{.KGDir}}          # Delete entity
kg link <from> <rel> <to> --dir={{.KGDir}}    # Create relation
kg unlink <from> <rel> <to> --dir={{.KGDir}}  # Remove relation
kg path <type:id> <rel> --dir={{.KGDir}}      # Traverse forward
kg back <type:id> --dir={{.KGDir}}            # Find reverse refs
kg validate --dir={{.KGDir}}                  # Validate all entities
` + "```" + `

## Output Formats

Default output is TOON (Token-Optimized Object Notation). Use flags:
- ` + "`--json`" + ` for JSON output
- ` + "`--human`" + ` for human-readable output

## Entity IDs

Entity IDs use the format ` + "`type:local-id`" + ` (e.g. ` + "`person:alice`" + `).

## Filter Syntax

` + "`kg find`" + ` supports filters: ` + "`=`" + `, ` + "`!=`" + `, ` + "`~`" + ` (contains), ` + "`>`" + `, ` + "`<`" + `, ` + "`>=`" + `, ` + "`<=`" + `

Example: ` + "`kg find person name~Ali --dir={{.KGDir}}`" + `
{{if .Types}}
## Available Types

{{range .Types}}- {{.}}
{{end}}{{end}}
## Extracting Facts from Text

When given text (conversation, documents, notes), extract structured
facts into the knowledge graph using this process:

1. **Identify entities** — look for named people, projects, tools,
   organizations, concepts, or topics that are worth remembering.
2. **Match to schema types** — check ` + "`kg types`" + ` and ` + "`kg describe <type>`" + `
   to find the right type. If no type fits, note the gap but don't
   force a bad fit.
3. **Check for duplicates** — before creating, run
   ` + "`kg find <type> name~<term> --dir={{.KGDir}}`" + ` to see if the
   entity already exists. Update existing entities with ` + "`kg set`" + `
   rather than creating duplicates.
4. **Extract relationships** — look for verbs and associations that map
   to schema relations (e.g. "Alice works at Acme" → ` + "`kg link`" + `).
5. **Prefer facts over opinions** — store things that are durable and
   verifiable. Skip transient states, small talk, and subjective
   judgments unless they reveal a preference or need.
6. **Use stable IDs** — derive IDs from the entity name using
   kebab-case (e.g. "Knowledge Graphs" → ` + "`knowledge-graphs`" + `).
   Keep IDs short and predictable so lookups work without searching.

### What to extract

- **People** and their roles, affiliations, preferences
- **Projects** and their purpose, status, relationships
- **Technologies** and how they connect to projects or people
- **Preferences** — "I like X", "we use Y", "avoid Z"
- **Decisions** — choices made and why
- **Facts** that provide context for future work

### What to skip

- Transient status ("I'm tired", "running late")
- Obvious/generic knowledge ("Python is a language")
- Anything already captured in the graph (check first)
- Vague statements with no concrete entity or fact

### Example

Given: "Paul mentioned he's been using LadybugDB for the graph index
and really likes how it handles storage."

Extract:
` + "```" + `bash
kg find topic name~LadybugDB --dir={{.KGDir}}   # check if exists
kg create topic ladybugdb name=LadybugDB category=technology --dir={{.KGDir}}
kg link person:paul likes topic:ladybugdb --dir={{.KGDir}}
` + "```" + `

## Health Check

Run ` + "`kg agent check --dir={{.KGDir}}`" + ` to verify graph health before operations.
`

// ---------------------------------------------------------------------------
// agent examples
// ---------------------------------------------------------------------------

func agentExamplesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "examples",
		Short: "Show example commands using current schema types",
		RunE: func(cmd *cobra.Command, args []string) error {
			schemas, _ := schema.LoadAllSchemas(cfg.kgDir)

			w := cmd.OutOrStdout()

			if len(schemas) == 0 {
				fmt.Fprintln(w, "# No schema types found. Create a schema first.")
				fmt.Fprintf(w, "# Place YAML schema files in %s/schema/\n", cfg.kgDir)
				return nil
			}

			// Pick the first type alphabetically for examples.
			names := make([]string, 0, len(schemas))
			for n := range schemas {
				names = append(names, n)
			}
			sort.Strings(names)
			typeName := names[0]
			s := schemas[typeName]

			fmt.Fprintf(w, "# Example workflow using type %q\n\n", typeName)

			// Build example attrs from schema.
			var attrPairs []string
			for name, def := range s.Attributes {
				attrPairs = append(attrPairs, fmt.Sprintf("%s=example-%s", name, def.Type))
			}
			sort.Strings(attrPairs)
			attrStr := strings.Join(attrPairs, " ")

			fmt.Fprintf(w, "# Create an entity\n")
			fmt.Fprintf(w, "kg create %s my-id %s --dir=%s\n\n", typeName, attrStr, cfg.kgDir)

			fmt.Fprintf(w, "# Get it back\n")
			fmt.Fprintf(w, "kg get %s:my-id --dir=%s\n\n", typeName, cfg.kgDir)

			fmt.Fprintf(w, "# Find all of this type\n")
			fmt.Fprintf(w, "kg find %s --dir=%s\n\n", typeName, cfg.kgDir)

			if len(attrPairs) > 0 {
				firstAttr := strings.SplitN(attrPairs[0], "=", 2)[0]
				fmt.Fprintf(w, "# Set an attribute\n")
				fmt.Fprintf(w, "kg set %s:my-id %s=new-value --dir=%s\n\n", typeName, firstAttr, cfg.kgDir)
			}

			// If there are relations, show link/path examples.
			if len(s.Relations) > 0 {
				relNames := make([]string, 0, len(s.Relations))
				for rn := range s.Relations {
					relNames = append(relNames, rn)
				}
				sort.Strings(relNames)
				rel := relNames[0]
				target := s.Relations[rel].Target

				fmt.Fprintf(w, "# Link to another entity\n")
				fmt.Fprintf(w, "kg link %s:my-id %s %s:other-id --dir=%s\n\n", typeName, rel, target, cfg.kgDir)

				fmt.Fprintf(w, "# Traverse the relation\n")
				fmt.Fprintf(w, "kg path %s:my-id %s --dir=%s\n\n", typeName, rel, cfg.kgDir)

				fmt.Fprintf(w, "# Find reverse references\n")
				fmt.Fprintf(w, "kg back %s:other-id --dir=%s\n\n", target, cfg.kgDir)
			}

			fmt.Fprintf(w, "# Validate all entities\n")
			fmt.Fprintf(w, "kg validate --dir=%s\n\n", cfg.kgDir)

			fmt.Fprintf(w, "# Delete the entity\n")
			fmt.Fprintf(w, "kg delete %s:my-id --dir=%s\n", typeName, cfg.kgDir)

			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// agent mcp-config
// ---------------------------------------------------------------------------

func agentMCPConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp-config",
		Short: "Print MCP server configuration for agent platforms",
		RunE: func(cmd *cobra.Command, args []string) error {
			githubFlag, _ := cmd.Flags().GetBool("github")
			claudeFlag, _ := cmd.Flags().GetBool("claude")

			absDir, err := filepath.Abs(cfg.kgDir)
			if err != nil {
				return err
			}

			// Find the kg binary.
			kgBin, err := os.Executable()
			if err != nil {
				kgBin = "kg"
			}

			w := cmd.OutOrStdout()

			switch {
			case githubFlag:
				config := map[string]any{
					"servers": map[string]any{
						"kg": map[string]any{
							"type":    "stdio",
							"command": kgBin,
							"args":    []string{"mcp", "serve", "--dir", absDir},
						},
					},
				}
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(config)

			case claudeFlag:
				config := map[string]any{
					"mcpServers": map[string]any{
						"kg": map[string]any{
							"command": kgBin,
							"args":    []string{"mcp", "serve", "--dir", absDir},
						},
					},
				}
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(config)

			default:
				config := map[string]any{
					"command": kgBin,
					"args":    []string{"mcp", "serve", "--dir", absDir},
					"transport": "stdio",
				}
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(config)
			}
		},
	}
	cmd.Flags().Bool("github", false, "emit config for .github/copilot/mcp.json")
	cmd.Flags().Bool("claude", false, "emit config for claude_desktop_config.json")
	return cmd
}

// ---------------------------------------------------------------------------
// agent check
// ---------------------------------------------------------------------------

func agentCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Health check for the knowledge graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			absDir, err := filepath.Abs(cfg.kgDir)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			var problems []string

			// Check kg dir exists.
			if _, err := os.Stat(cfg.kgDir); os.IsNotExist(err) {
				problems = append(problems, "kg directory not found: "+cfg.kgDir)
			}

			// Index freshness.
			idxStatus := indexFreshness(cfg.kgDir)
			if idxStatus != "current" {
				problems = append(problems, "index: "+idxStatus)
			}

			// Daemon status.
			dStatus := daemonRunning(absDir)
			if dStatus != "running" {
				problems = append(problems, "daemon: "+dStatus)
			}

			// Validation errors.
			schemas, schemaErr := schema.LoadAllSchemas(cfg.kgDir)
			entities, entityErr := store.ListEntities(cfg.kgDir)
			if schemaErr == nil && entityErr == nil {
				for _, e := range entities {
					s, ok := schemas[e.Type]
					if !ok {
						continue
					}
					errs := schema.ValidateEntity(s, e)
					for _, ve := range errs {
						problems = append(problems, fmt.Sprintf("validation: %s: %s: %s", e.ID, ve.Field, ve.Message))
					}
				}
			}

			result := checkResult{
				Healthy:  len(problems) == 0,
				Backend:  store.ActiveBackend(cfg.kgDir),
				Problems: problems,
			}

			if err := writeOutput(w, result); err != nil {
				return err
			}

			if !result.Healthy {
				return fmt.Errorf("health check failed: %d problem(s)", len(problems))
			}
			return nil
		},
	}
}

type checkResult struct {
	Healthy  bool     `json:"healthy"`
	Backend  string   `json:"backend"`
	Problems []string `json:"problems,omitempty"`
}

func (r checkResult) toTOON(w io.Writer) {
	fmt.Fprintf(w, "healthy: %v\n", r.Healthy)
	fmt.Fprintf(w, "backend: %s\n", r.Backend)
	if len(r.Problems) > 0 {
		fmt.Fprintf(w, "problems[%d]:\n", len(r.Problems))
		for _, p := range r.Problems {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type toonWriter interface {
	toTOON(w io.Writer)
}

func writeOutput(w io.Writer, v any) error {
	switch cfg.format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	default:
		if tw, ok := v.(toonWriter); ok {
			tw.toTOON(w)
			return nil
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
}

func indexFreshness(kgDir string) string {
	manifest, err := store.LoadManifest(kgDir)
	if err != nil {
		return "missing"
	}
	if len(manifest.Files) == 0 {
		return "empty"
	}

	// Compare manifest entries against actual entity files.
	entities, err := store.ListEntities(kgDir)
	if err != nil {
		return "unknown"
	}
	if len(entities) == 0 && len(manifest.Files) == 0 {
		return "current"
	}
	if len(entities) != len(manifest.Files) {
		return "stale"
	}
	return "current"
}

func daemonRunning(absDir string) string {
	sockPath := daemon.SocketPathFor(absDir)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return "stopped"
	}
	defer conn.Close()
	return "running"
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
