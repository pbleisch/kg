package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

			return fmt.Errorf("apply execution not implemented yet; use --dry-run to simulate")
		},
	}
	cmd.Flags().Bool("dry-run", false, "Simulate the manifest without applying changes")
	cmd.Flags().String("on-conflict", "fail", "Behavior when conflicts are encountered: fail, skip, merge")
	rootCmd.AddCommand(cmd)
}
