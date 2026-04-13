package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Manifest represents the top-level apply manifest.
type Manifest struct {
	Version        int                    `yaml:"version" json:"version"`
	IdempotencyKey string                 `yaml:"idempotency_key,omitempty" json:"idempotency_key,omitempty"`
	Actions        []Action               `yaml:"actions" json:"actions"`
	Options        map[string]interface{} `yaml:"options,omitempty" json:"options,omitempty"`
}

// Action is a single operation in the manifest.
type Action struct {
	ID         string                 `yaml:"id,omitempty" json:"id,omitempty"`
	Action     string                 `yaml:"action" json:"action"`
	Entity     *store.Entity          `yaml:"entity,omitempty" json:"entity,omitempty"`
	Target     string                 `yaml:"target,omitempty" json:"target,omitempty"`
	From       string                 `yaml:"from,omitempty" json:"from,omitempty"`
	To         string                 `yaml:"to,omitempty" json:"to,omitempty"`
	Rel        string                 `yaml:"rel,omitempty" json:"rel,omitempty"`
	Prefer     string                 `yaml:"prefer,omitempty" json:"prefer,omitempty"`
	Attributes map[string]interface{} `yaml:"attributes,omitempty" json:"attributes,omitempty"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "apply <manifest>",
		Short: "Apply a manifest of actions (supports --dry-run)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading manifest: %w", err)
			}
			var m Manifest
			if err := yaml.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("parsing manifest: %w", err)
			}
			if m.Version == 0 {
				m.Version = 1
			}
			if m.Version != 1 {
				return fmt.Errorf("unsupported manifest version: %d", m.Version)
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			onConflict, _ := cmd.Flags().GetString("on-conflict")
			onError, _ := cmd.Flags().GetString("on-error")
			dedupThreshold, _ := cmd.Flags().GetFloat64("dedup-threshold")
			dedupFieldsStr, _ := cmd.Flags().GetString("dedup-fields")

			// Validate on-conflict flag.
			switch onConflict {
			case "fail", "skip", "merge", "dedup":
				// ok
			default:
				return fmt.Errorf("invalid on-conflict value: %s", onConflict)
			}

			if onConflict == "dedup" && dedupFieldsStr == "" {
				return fmt.Errorf("--dedup-fields is required when using --on-conflict dedup")
			}

			// Validate on-error flag.
			switch onError {
			case "fail", "skip", "warn":
				// ok
			default:
				return fmt.Errorf("invalid on-error value: %s", onError)
			}

			// Build summary.
			summary := map[string]interface{}{
				"version":       m.Version,
				"total_actions": len(m.Actions),
				"counts":        map[string]int{},
				"action_ids":    []string{},
				"on_conflict":   onConflict,
			}
			counts := summary["counts"].(map[string]int)
			ids := summary["action_ids"].([]string)
			for _, a := range m.Actions {
				action := strings.ToLower(a.Action)
				counts[action]++
				if a.ID != "" {
					ids = append(ids, a.ID)
				} else if a.Entity != nil && a.Entity.ID != "" {
					ids = append(ids, a.Entity.ID)
				} else if a.Target != "" {
					ids = append(ids, a.Target)
				} else {
					ids = append(ids, "(unnamed)")
				}
			}
			summary["action_ids"] = ids

			// Output summary according to requested format.
			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(summary); err != nil {
					return fmt.Errorf("encoding summary: %w", err)
				}
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "Manifest version: %d\n", m.Version)
				fmt.Fprintf(cmd.OutOrStdout(), "Total actions: %d\n", len(m.Actions))
				for k, v := range counts {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d\n", k, v)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Action ids:")
				for _, id := range ids {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", id)
				}
			}

			if dryRun {
				// Nothing else to do for dry-run.
				return nil
			}

			// Execute actions sequentially.
			for i := range m.Actions {
				normalizeAction(&m.Actions[i])
			}

			// Parse dedup fields once if needed.
			var dedupFields []string
			if onConflict == "dedup" {
				for _, f := range strings.Split(dedupFieldsStr, ",") {
					f = strings.TrimSpace(f)
					if f != "" {
						dedupFields = append(dedupFields, f)
					}
				}
			}

			// Pre-compute embeddings for all dedup entities in a single batch
			// call, avoiding N separate subprocess invocations.
			if onConflict == "dedup" && len(dedupFields) > 0 {
				ecfg, err := store.LoadConfig(cfg.kgDir)
				if err == nil && ecfg.Embedding != nil && ecfg.Embedding.Command != "" {
					var inputs []store.EmbedInput
					type embedTarget struct {
						actionIdx int
						field     string
					}
					var targets []embedTarget

					for i, a := range m.Actions {
						act := strings.ToLower(a.Action)
						if act != "create" && act != "upsert" {
							continue
						}
						if a.Entity == nil {
							continue
						}
						for _, field := range dedupFields {
							embKey := field + "_embedding"
							if val, ok := a.Entity.Attributes[embKey]; ok {
								if vec, ok := store.ExtractVector(val); ok && len(vec) > 0 {
									continue // already has embedding
								}
							}
							text, ok := a.Entity.Attributes[field]
							if !ok {
								continue
							}
							textStr := fmt.Sprintf("%v", text)
							if textStr == "" {
								continue
							}
							typeName := a.Entity.Type
							if typeName == "" {
								if parts := strings.SplitN(a.Entity.ID, ":", 2); len(parts) == 2 {
									typeName = parts[0]
								}
							}
							inputs = append(inputs, store.EmbedInput{
								Text:     textStr,
								Field:    field,
								EntityID: a.Entity.ID,
								Type:     typeName,
							})
							targets = append(targets, embedTarget{actionIdx: i, field: field})
						}
					}

					if len(inputs) > 0 {
						if vectors, err := store.RunEmbedBatch(ecfg.Embedding.Command, inputs); err == nil && len(vectors) == len(inputs) {
							for i, t := range targets {
								embKey := t.field + "_embedding"
								m.Actions[t.actionIdx].Entity.Attributes[embKey] = vectors[i]
							}
							if !cfg.quiet {
								fmt.Fprintf(os.Stderr, "  pre-embedded %d fields for dedup\n", len(vectors))
							}
						}
					}
				}
			}

			// ID remap table for dedup: proposed_id → existing_id.
			idRemap := make(map[string]string)
			var dedupCount int
			var applyErrors []string

			// Open index once for dedup vector search and merge referrer lookups.
			var applyIdx store.Indexer
			if idx, err := store.OpenIndex(cfg.kgDir); err == nil {
				applyIdx = idx
			}

			store.BeginBatch()
			for _, a := range m.Actions {
				// Rewrite IDs through remap table (dedup mode).
				if len(idRemap) > 0 {
					if a.From != "" {
						if mapped, ok := idRemap[a.From]; ok {
							a.From = mapped
						}
					}
					if a.To != "" {
						if mapped, ok := idRemap[a.To]; ok {
							a.To = mapped
						}
					}
					if a.Target != "" {
						if mapped, ok := idRemap[a.Target]; ok {
							a.Target = mapped
						}
					}
				}

				var actionErr error
				switch strings.ToLower(a.Action) {
				case "create":
					if onConflict == "dedup" {
						var remapTo string
						actionErr, remapTo = applyCreateDedup(a, dedupFields, dedupThreshold, applyIdx)
						if remapTo != "" && a.Entity != nil {
							idRemap[a.Entity.ID] = remapTo
							dedupCount++
							if !cfg.quiet {
								fmt.Fprintf(os.Stderr, "  dedup: %s → %s\n", a.Entity.ID, remapTo)
							}
						}
					} else {
						actionErr = applyCreate(a, onConflict)
					}
				case "upsert":
					if onConflict == "dedup" {
						var remapTo string
						actionErr, remapTo = applyCreateDedup(a, dedupFields, dedupThreshold, applyIdx)
						if remapTo != "" && a.Entity != nil {
							idRemap[a.Entity.ID] = remapTo
							dedupCount++
							if !cfg.quiet {
								fmt.Fprintf(os.Stderr, "  dedup: %s → %s\n", a.Entity.ID, remapTo)
							}
						}
					} else {
						if a.Entity == nil || a.Entity.ID == "" {
							actionErr = fmt.Errorf("upsert action missing entity or id")
						} else if _, err := command.UpsertEntity(cfg.kgDir, a.Entity); err != nil {
							actionErr = fmt.Errorf("upserting %q: %w", a.Entity.ID, err)
						}
					}
				case "set":
					actionErr = applySet(a)
				case "delete":
					if a.Target == "" {
						actionErr = fmt.Errorf("delete action missing target id")
					} else {
						actionErr = command.Delete(cfg.kgDir, a.Target)
					}
				case "link":
					if a.From == "" || a.Rel == "" || a.To == "" {
						actionErr = fmt.Errorf("link action missing from/rel/to")
					} else {
						actionErr = command.Link(cfg.kgDir, a.From, a.Rel, a.To)
					}
				case "unlink":
					if a.From == "" || a.Rel == "" || a.To == "" {
						actionErr = fmt.Errorf("unlink action missing from/rel/to")
					} else {
						actionErr = command.Unlink(cfg.kgDir, a.From, a.Rel, a.To)
					}
				case "merge":
					if a.From == "" || a.Target == "" {
						actionErr = fmt.Errorf("merge action missing from/target")
					} else {
						opts := command.MergeOptions{
							PreferSource: a.Prefer == "source",
							CrossType:    true,
							Idx:          applyIdx,
						}
						if _, err := command.Merge(cfg.kgDir, a.From, a.Target, opts); err != nil {
							actionErr = fmt.Errorf("merging %s→%s: %w", a.From, a.Target, err)
						}
					}
				default:
					actionErr = fmt.Errorf("unknown action %q", a.Action)
				}

				if actionErr != nil {
					label := a.Action
					if a.Entity != nil && a.Entity.ID != "" {
						label += " " + a.Entity.ID
					} else if a.Target != "" {
						label += " " + a.Target
					} else if a.From != "" {
						label += " " + a.From
					}
					switch onError {
					case "fail":
						return fmt.Errorf("action %q: %w", label, actionErr)
					case "warn":
						fmt.Fprintf(os.Stderr, "  warning: %s: %s\n", label, actionErr)
						applyErrors = append(applyErrors, label+": "+actionErr.Error())
					case "skip":
						applyErrors = append(applyErrors, label+": "+actionErr.Error())
					}
				}
			}
			if len(applyErrors) > 0 && !cfg.quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "%d action(s) failed\n", len(applyErrors))
			}
			if dedupCount > 0 && !cfg.quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "%d action(s) deduped\n", dedupCount)
			}
			// Close index before final sync to release lock.
			if applyIdx != nil {
				applyIdx.Close()
				applyIdx = nil
			}
			// End batch mode — triggers a single final sync.
			if err := store.EndBatch(cfg.kgDir); err != nil {
				// Non-fatal: YAML files are already written.
				if !cfg.quiet {
					fmt.Fprintf(os.Stderr, "warning: post-apply sync failed (run 'kg sync' manually): %v\n", err)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "Simulate the manifest without applying changes")
	cmd.Flags().String("on-conflict", "fail", "Behavior when conflicts are encountered: fail, skip, merge, dedup")
	cmd.Flags().String("on-error", "warn", "Behavior on action errors: fail (abort), warn (continue with stderr), skip (continue silently)")
	cmd.Flags().Float64("dedup-threshold", 0.85, "Similarity threshold for dedup mode (0.0–1.0)")
	cmd.Flags().String("dedup-fields", "", "Comma-separated fields for dedup similarity (required with --on-conflict dedup)")
	rootCmd.AddCommand(cmd)
}

// applyCreate handles a create action with conflict resolution.
func applyCreate(a Action, onConflict string) error {
	if a.Entity == nil || a.Entity.ID == "" {
		return fmt.Errorf("create action missing entity or id")
	}
	if _, err := store.ReadEntity(cfg.kgDir, a.Entity.ID); err == nil {
		switch onConflict {
		case "fail":
			return fmt.Errorf("entity %q already exists", a.Entity.ID)
		case "skip":
			return nil
		case "merge":
			existing, err := store.ReadEntity(cfg.kgDir, a.Entity.ID)
			if err != nil {
				return fmt.Errorf("reading existing entity %q: %w", a.Entity.ID, err)
			}
			if existing.Attributes == nil {
				existing.Attributes = make(map[string]any)
			}
			for k, v := range a.Entity.Attributes {
				existing.Attributes[k] = v
			}
			for rel, targets := range a.Entity.Relations {
				if existing.Relations == nil {
					existing.Relations = make(map[string][]string)
				}
				for _, t := range targets {
					found := false
					for _, ex := range existing.Relations[rel] {
						if ex == t {
							found = true
							break
						}
					}
					if !found {
						existing.Relations[rel] = append(existing.Relations[rel], t)
					}
				}
			}
			if err := store.WriteEntity(cfg.kgDir, existing); err != nil {
				return fmt.Errorf("writing merged entity %q: %w", a.Entity.ID, err)
			}
			return nil
		}
	}
	if err := store.WriteEntity(cfg.kgDir, a.Entity); err != nil {
		return fmt.Errorf("creating entity %q: %w", a.Entity.ID, err)
	}
	return nil
}

// applySet handles a set action.
func applySet(a Action) error {
	if a.Target == "" {
		return fmt.Errorf("set action missing target id")
	}
	e, err := store.ReadEntity(cfg.kgDir, a.Target)
	if err != nil {
		return fmt.Errorf("reading target %q: %w", a.Target, err)
	}
	if e.Attributes == nil {
		e.Attributes = make(map[string]any)
	}
	for k, v := range a.Attributes {
		e.Attributes[k] = v
	}
	if err := store.WriteEntity(cfg.kgDir, e); err != nil {
		return fmt.Errorf("writing target %q: %w", a.Target, err)
	}
	return nil
}

// normalizeAction applies snake_case normalization to all IDs and relation
// names in an action before dispatch.
func normalizeAction(a *Action) {
	if a.Entity != nil {
		a.Entity.ID = store.NormalizeEntityID(a.Entity.ID)
		a.Entity.Type = store.ToSnakeCase(a.Entity.Type)
		if a.Entity.Relations != nil {
			normed := make(map[string][]string, len(a.Entity.Relations))
			for rel, targets := range a.Entity.Relations {
				nTargets := make([]string, len(targets))
				for i, t := range targets {
					nTargets[i] = store.NormalizeEntityID(t)
				}
				normed[store.NormalizeRelation(rel)] = nTargets
			}
			a.Entity.Relations = normed
		}
	}
	if a.Target != "" {
		a.Target = store.NormalizeEntityID(a.Target)
	}
	if a.From != "" {
		a.From = store.NormalizeEntityID(a.From)
	}
	if a.To != "" {
		a.To = store.NormalizeEntityID(a.To)
	}
	if a.Rel != "" {
		a.Rel = store.NormalizeRelation(a.Rel)
	}
}

// applyCreateDedup handles create/upsert with similarity-aware dedup.
// Returns (error, remappedToID). If remappedToID is non-empty, the entity
// was deduped into an existing entity instead of being created.
func applyCreateDedup(a Action, dedupFields []string, threshold float64, dedupIdx store.Indexer) (error, string) {
	if a.Entity == nil || a.Entity.ID == "" {
		return fmt.Errorf("create/upsert action missing entity or id"), ""
	}

	typeName := a.Entity.Type
	if typeName == "" {
		// Infer type from ID.
		parts := strings.SplitN(a.Entity.ID, ":", 2)
		if len(parts) == 2 {
			typeName = parts[0]
		}
	}

	// Build query vectors from the new entity's attributes.
	// Pre-computed embeddings (from batch embed or manifest) are used directly.
	queryVecs := make(map[string][]float64)
	for _, field := range dedupFields {
		embKey := field + "_embedding"
		if val, ok := a.Entity.Attributes[embKey]; ok {
			if vec, ok := store.ExtractVector(val); ok && len(vec) > 0 {
				queryVecs[field] = vec
			}
		}
	}

	if len(queryVecs) == 0 {
		// No vectors available — fall through to normal create/upsert.
		return createEntity(a), ""
	}

	// Use index-based vector search if available (much faster than YAML scan).
	if dedupIdx != nil {
		if vs, ok := dedupIdx.(store.VectorSearcher); ok {
			bestScore := 0.0
			bestID := ""
			var mu sync.Mutex
			var wg sync.WaitGroup
			for _, field := range dedupFields {
				qv, ok := queryVecs[field]
				if !ok {
					continue
				}
				wg.Add(1)
				go func(f string, v []float64) {
					defer wg.Done()
					results, err := vs.SimilarEntities(store.SimilarityParams{
						TypeName: typeName,
						Fields:   []string{f},
						Vector:   v,
						TopN:     1,
					})
					if err == nil && len(results) > 0 {
						mu.Lock()
						if results[0].Score > bestScore {
							bestScore = results[0].Score
							bestID = results[0].Entity.ID
						}
						mu.Unlock()
					}
				}(field, qv)
			}
			wg.Wait()
			if bestScore >= threshold && bestID != "" && bestID != a.Entity.ID {
				mergeOpts := command.MergeOptions{CrossType: true, Idx: dedupIdx}
				if err := store.WriteEntity(cfg.kgDir, a.Entity); err != nil {
					return fmt.Errorf("writing entity for merge: %w", err), ""
				}
				_, err := command.Merge(cfg.kgDir, a.Entity.ID, bestID, mergeOpts)
				if err != nil {
					return fmt.Errorf("dedup merge %s → %s: %w", a.Entity.ID, bestID, err), ""
				}
				return nil, bestID
			}
			return createEntity(a), ""
		}
	}

	// Fallback: brute-force YAML scan (slow on large KGs).
	existing, err := store.ListEntitiesByType(cfg.kgDir, typeName)
	if err != nil {
		return createEntity(a), ""
	}

	// Find best match among existing entities.
	bestScore := 0.0
	bestID := ""
	bestField := ""
	for _, e := range existing {
		if e.ID == a.Entity.ID {
			continue
		}
		for _, field := range dedupFields {
			qv, ok := queryVecs[field]
			if !ok {
				continue
			}
			embKey := field + "_embedding"
			val, ok := e.Attributes[embKey]
			if !ok {
				continue
			}
			vec, ok := store.ExtractVector(val)
			if !ok || len(vec) == 0 {
				continue
			}
			score, err := store.CosineSimilarity(qv, vec)
			if err != nil {
				continue
			}
			if score > bestScore {
				bestScore = score
				bestID = e.ID
				bestField = field
			}
		}
	}

	if bestScore >= threshold && bestID != "" {
		// Dedup: merge into existing entity.
		_ = bestField
		mergeOpts := command.MergeOptions{CrossType: true, Idx: dedupIdx}
		// First create the entity so Merge can read it.
		if err := store.WriteEntity(cfg.kgDir, a.Entity); err != nil {
			return fmt.Errorf("writing entity for merge: %w", err), ""
		}
		_, err := command.Merge(cfg.kgDir, a.Entity.ID, bestID, mergeOpts)
		if err != nil {
			return fmt.Errorf("dedup merge %s → %s: %w", a.Entity.ID, bestID, err), ""
		}
		return nil, bestID
	}

	// No match above threshold — create normally.
	return createEntity(a), ""
}

// createEntity writes an entity to disk (sync deferred to end of apply).
func createEntity(a Action) error {
	if err := store.WriteEntity(cfg.kgDir, a.Entity); err != nil {
		return fmt.Errorf("creating entity %q: %w", a.Entity.ID, err)
	}
	return nil
}
