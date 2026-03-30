package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

			// Validate on-conflict flag.
			switch onConflict {
			case "fail", "skip", "merge":
				// ok
			default:
				return fmt.Errorf("invalid on-conflict value: %s", onConflict)
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
			for _, a := range m.Actions {
				switch strings.ToLower(a.Action) {
				case "create":
					if a.Entity == nil || a.Entity.ID == "" {
						return fmt.Errorf("create action missing entity or id")
					}
					// Check existence.
					if _, err := store.ReadEntity(cfg.kgDir, a.Entity.ID); err == nil {
						// exists
						switch onConflict {
						case "fail":
							return fmt.Errorf("entity %q already exists", a.Entity.ID)
						case "skip":
							continue
						case "merge":
							// Merge attributes/relations into existing entity.
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
							if err := store.Sync(cfg.kgDir); err != nil {
								return fmt.Errorf("syncing index: %w", err)
							}
							continue
						}
					}
					// Not exists: write entity directly.
					if err := store.WriteEntity(cfg.kgDir, a.Entity); err != nil {
						return fmt.Errorf("creating entity %q: %w", a.Entity.ID, err)
					}
					if err := store.Sync(cfg.kgDir); err != nil {
						return fmt.Errorf("syncing index: %w", err)
					}
				case "set":
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
					if err := store.Sync(cfg.kgDir); err != nil {
						return fmt.Errorf("syncing index: %w", err)
					}
				case "delete":
					if a.Target == "" {
						return fmt.Errorf("delete action missing target id")
					}
					if err := command.Delete(cfg.kgDir, a.Target); err != nil {
						return fmt.Errorf("deleting %q: %w", a.Target, err)
					}
				case "link":
					if a.From == "" || a.Rel == "" || a.To == "" {
						return fmt.Errorf("link action missing from/rel/to")
					}
					if err := command.Link(cfg.kgDir, a.From, a.Rel, a.To); err != nil {
						return fmt.Errorf("link %s->%s (%s): %w", a.From, a.To, a.Rel, err)
					}
				case "unlink":
					if a.From == "" || a.Rel == "" || a.To == "" {
						return fmt.Errorf("unlink action missing from/rel/to")
					}
					if err := command.Unlink(cfg.kgDir, a.From, a.Rel, a.To); err != nil {
						return fmt.Errorf("unlink %s->%s (%s): %w", a.From, a.To, a.Rel, err)
					}
				case "upsert":
					if a.Entity == nil || a.Entity.ID == "" {
						return fmt.Errorf("upsert action missing entity or id")
					}
					if _, err := command.UpsertEntity(cfg.kgDir, a.Entity); err != nil {
						return fmt.Errorf("upserting %q: %w", a.Entity.ID, err)
					}
				case "merge":
					if a.From == "" || a.Target == "" {
						return fmt.Errorf("merge action missing from/target")
					}
					opts := command.MergeOptions{
						PreferSource: a.Prefer == "source",
						CrossType:    true,
					}
					if _, err := command.Merge(cfg.kgDir, a.From, a.Target, opts); err != nil {
						return fmt.Errorf("merging %s→%s: %w", a.From, a.Target, err)
					}
				default:
					return fmt.Errorf("unknown action %q", a.Action)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "Simulate the manifest without applying changes")
	cmd.Flags().String("on-conflict", "fail", "Behavior when conflicts are encountered: fail, skip, merge")
	rootCmd.AddCommand(cmd)
}
