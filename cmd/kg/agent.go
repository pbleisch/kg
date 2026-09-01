package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/pbleisch/kg/pkg/vector"
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

			status := agentStatus{Dir: absDir, Graph: "loads"}

			// Entity counts by type. A read that refuses is a refusal and
			// never a miss (ADR-0028): the count that follows a swallowed
			// LoadError is zero, and zero entities is what an empty graph
			// looks like. Say "refused" and carry the error out so this
			// exits 2, the way every other read of a refused corpus does.
			var refusal error
			entities, err := store.ListEntities(cfg.kgDir)
			switch errs, refused := store.LoadRefused(err); {
			case refused:
				status.Graph = fmt.Sprintf("refused: %d structural error(s)", len(errs))
				refusal = err
			case err != nil:
				return err
			default:
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

			// Derived data: the vector sidecar, and whether an embedder is
			// configured to rebuild it. There is no index and no daemon to
			// report on; a read needs neither.
			status.Vectors = vectorStatus(cfg.kgDir)
			status.Embedder = embedderStatus(cfg.kgDir)

			if err := writeOutput(cmd.OutOrStdout(), status); err != nil {
				return err
			}
			// Written first, so an agent orienting on a broken graph still
			// gets the dir and the schema types it needs to repair it.
			return refusal
		},
	}
}

type agentStatus struct {
	Dir string `json:"dir"`
	// Graph says whether the corpus loads. A refused corpus reports
	// "refused: N structural error(s)" and the counts below are absent, not
	// zero.
	Graph          string         `json:"graph"`
	EntityCount    int            `json:"entity_count"`
	EntitiesByType map[string]int `json:"entities_by_type,omitempty"`
	SchemaTypes    []string       `json:"schema_types,omitempty"`
	// Vectors describes the gitignored vector sidecar `kg similar` reads.
	Vectors string `json:"vectors"`
	// Embedder says where the embedding command came from, or that none is
	// configured. kg never picks one for you (P8).
	Embedder string `json:"embedder"`
}

func (s agentStatus) toTOON(w io.Writer) {
	fmt.Fprintf(w, "dir: %s\n", s.Dir)
	fmt.Fprintf(w, "graph: %s\n", s.Graph)
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
	fmt.Fprintf(w, "vectors: %s\n", s.Vectors)
	fmt.Fprintf(w, "embedder: %s\n", s.Embedder)
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
  Available commands: create, get, find, set, unset, delete, link, unlink, path, back, describe,
  types, validate, resolve.
---

# Using kg (Knowledge Graph CLI)

## Quick Reference

` + "```" + `bash
kg types --dir={{.KGDir}}                     # List schema types
kg describe <type> --dir={{.KGDir}}           # Show type schema
kg create <type> <id> [attrs...] --dir={{.KGDir}}  # Create entity
kg get <type:id> --dir={{.KGDir}}             # Get entity by ID
kg find <type> [filters...] --dir={{.KGDir}}  # Find entities
kg set <type:id> key=value --dir={{.KGDir}}   # Assign attributes (overwrites)
kg unset <type:id> <attr...> --dir={{.KGDir}} # Remove attributes
kg delete <type:id> --dir={{.KGDir}}          # Delete entity
kg link <from> <rel> <to> --dir={{.KGDir}}    # Create relation
kg unlink <from> <rel> <to> --dir={{.KGDir}}  # Remove relation
kg path <type:id> <rel> --dir={{.KGDir}}      # Traverse forward
kg back <type:id> --dir={{.KGDir}}            # Find reverse refs
kg validate --dir={{.KGDir}}                  # Structure fails, content warns
kg validate --fix --dir={{.KGDir}}            # Repair id/type and illegal names
kg resolve --dir={{.KGDir}}                   # Pairs that may be one entity
` + "```" + `

## What a load refuses

An entity's identity is where its file sits: ` + "`entities/<type>/<local>.yaml`" + ` is
` + "`<type>:<local>`" + `. Four things refuse the whole graph, and no flag suppresses
them: an identity two files claim, a declared ` + "`id:`" + ` or ` + "`type:`" + ` that
disagrees with the path, an attribute name outside ` + "`[a-z][a-z0-9_]*`" + `, and a
file that will not parse. ` + "`kg validate --fix`" + ` repairs the last three classes
in place and moves no file.

Everything else is a warning and the graph still loads — an unknown attribute, a
schema violation, and a relation target that names nothing.

## What happens when two records claim one identity

kg has one merge rule and both verbs that combine two claims use it — an upsert
landing on an existing entity, and ` + "`kg merge`" + `. The rule is by field kind, and
the kind is what the type's schema declares (ADR-0030):

- a **scalar** attribute takes the incoming value, and overwriting a *different*
  one is reported as ` + "`attribute_overwritten`" + `.
- an attribute the schema declares ` + "`type: list`" + ` is **multi-valued**. Its items
  carry ` + "`{text, source}`" + `, and a write **replaces the items its own contributor
  wrote before and leaves every other contributor's standing**. A scalar arriving
  at such a field is a one-element list; a string is never split on a separator.
- a **relation** unions.

Two records of one batch claiming one identity are **folded into one** before
anything is written, not refused: a document extracted in sections names its
subject in more than one of them. A ` + "`create`" + ` under ` + "`--on-conflict=fail`" + ` still
refuses, because a create asserts novelty.

A batch names its contributor in the manifest (` + "`contributor: source:<id>`" + `); that
is the unit of reset. A write with no contributor lands items carrying no
source, which no later write replaces — so a hand-authored item survives every
re-ingest, and ` + "`kg unset`" + ` is what removes it.

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

Given: "Paul mentioned he's been using SQLite for the reporting job
and really likes how it handles concurrent readers."

Extract:
` + "```" + `bash
kg find topic name~SQLite --dir={{.KGDir}}   # check if exists
kg create topic sqlite name=SQLite category=technology --dir={{.KGDir}}
kg link person:paul likes topic:sqlite --dir={{.KGDir}}
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
					"command":   kgBin,
					"args":      []string{"mcp", "serve", "--dir", absDir},
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
			w := cmd.OutOrStdout()
			var result checkResult
			var problems []string

			// Check kg dir exists.
			if _, err := os.Stat(cfg.kgDir); os.IsNotExist(err) {
				problems = append(problems, "kg directory not found: "+cfg.kgDir)
			}

			// Derived data. An absent sidecar is only a problem when an
			// embedder is configured to build one; a graph that never
			// embedded anything is complete without it.
			if _, err := store.ResolveEmbedder(cfg.kgDir); err != nil {
				var missing *store.ErrNoEmbedder
				if !errors.As(err, &missing) {
					problems = append(problems, "embedder: "+err.Error())
				}
			} else if v := vectorStatus(cfg.kgDir); v != "present" {
				problems = append(problems, "vectors: "+v+" (run 'kg sync')")
			}

			// Both halves of P3, from the one place that states them.
			// Hand-rolling schema.ValidateEntity here is how this command
			// came to answer "healthy: true" on a corpus kg refuses: it
			// walked with a read that refuses, dropped the refusal, and
			// validated the empty list that came back. query.Validate is
			// the same call `kg validate` makes, so the two surfaces cannot
			// disagree again (P7 thinness).
			rep, err := query.Validate(cfg.kgDir)
			if err != nil {
				return err
			}
			var advisories []string
			if rep != nil {
				result.Files, result.Entities = rep.Files, rep.Entities
				for _, st := range rep.Structural {
					problems = append(problems, fmt.Sprintf("%s: %s", st.Code, st.Error()))
				}
				for _, a := range rep.Advisories {
					advisories = append(advisories, fmt.Sprintf("%s: %s", a.Code, a.String()))
				}
			}

			result.Healthy = len(problems) == 0
			result.Problems = problems
			result.Advisories = advisories

			if err := writeOutput(w, result); err != nil {
				return err
			}

			// A corpus that will not load exits 2 with the library's own
			// error, exactly as `kg validate`, `kg find` and `kg stats` do.
			// Advisories are the advised half and never refuse (ADR-0028).
			if rep != nil && !rep.OK() {
				return &store.LoadError{Errors: rep.Structural}
			}
			if !result.Healthy {
				return fmt.Errorf("health check failed: %d problem(s)", len(problems))
			}
			return nil
		},
	}
}

// A checkResult keeps P3's two halves apart, because they are two rules with
// two severities and must not share a field: Problems refuse the graph,
// Advisories are reported and refuse nothing.
type checkResult struct {
	Healthy    bool     `json:"healthy"`
	Files      int      `json:"files"`
	Entities   int      `json:"entities"`
	Problems   []string `json:"problems,omitempty"`
	Advisories []string `json:"advisories,omitempty"`
}

func (r checkResult) toTOON(w io.Writer) {
	fmt.Fprintf(w, "healthy: %v\n", r.Healthy)
	fmt.Fprintf(w, "files: %d\n", r.Files)
	fmt.Fprintf(w, "entities: %d\n", r.Entities)
	if len(r.Problems) > 0 {
		fmt.Fprintf(w, "problems[%d]:\n", len(r.Problems))
		for _, p := range r.Problems {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	if len(r.Advisories) > 0 {
		fmt.Fprintf(w, "advisories[%d]:\n", len(r.Advisories))
		for _, a := range r.Advisories {
			fmt.Fprintf(w, "  %s\n", a)
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

// vectorStatus describes the vector sidecar in one word an agent can branch on.
func vectorStatus(kgDir string) string {
	digest := ""
	if emb, err := store.ResolveEmbedder(kgDir); err == nil {
		digest = emb.Digest()
	}
	vs, err := vector.Open(store.CacheDir(kgDir), digest)
	if err != nil {
		var missing *vector.ErrNoSidecar
		if errors.As(err, &missing) {
			return "absent"
		}
		return "stale"
	}
	defer vs.Close()
	return "present"
}

// embedderStatus says where the embedding command came from, so an agent that
// finds no vectors can tell "nobody configured one" from "it is misconfigured".
func embedderStatus(kgDir string) string {
	emb, err := store.ResolveEmbedder(kgDir)
	if err != nil {
		var missing *store.ErrNoEmbedder
		if errors.As(err, &missing) {
			return "not configured"
		}
		return "error: " + err.Error()
	}
	return emb.Source
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
