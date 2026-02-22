package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import an entity YAML/JSON file preserving typed attributes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}
			var e store.Entity
			trim := strings.TrimSpace(string(data))
			if strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[") {
				if err := json.Unmarshal(data, &e); err != nil {
					return fmt.Errorf("parsing json: %w", err)
				}
			} else {
				if err := yaml.Unmarshal(data, &e); err != nil {
					return fmt.Errorf("parsing yaml: %w", err)
				}
			}

			if e.ID == "" {
				return fmt.Errorf("entity missing id")
			}
			if e.Type == "" {
				return fmt.Errorf("entity missing type")
			}

			// Validate against schema if present.
			parts := strings.SplitN(e.ID, ":", 2)
			if len(parts) >= 1 {
				typeName := parts[0]
				if s, err := schema.LoadSchema(cfg.kgDir, typeName); err == nil {
					if errs := schema.ValidateEntity(s, &e); len(errs) > 0 {
						msgs := make([]string, len(errs))
						for i, ve := range errs {
							msgs[i] = ve.Error()
						}
						return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
					}
				} else if !os.IsNotExist(err) && err != nil {
					return fmt.Errorf("loading schema: %w", err)
				}
			}

			if err := store.WriteEntity(cfg.kgDir, &e); err != nil {
				return fmt.Errorf("writing entity: %w", err)
			}
			if err := store.Sync(cfg.kgDir); err != nil {
				return fmt.Errorf("syncing index: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), e.ID)
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
