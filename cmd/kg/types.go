package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "types",
		Short: "List all known schema types",
		RunE: func(cmd *cobra.Command, args []string) error {
			schemas, err := schema.LoadAllSchemas(cfg.kgDir)
			if err != nil {
				return fmt.Errorf("loading schemas: %w", err)
			}
			names := make([]string, 0, len(schemas))
			for name := range schemas {
				names = append(names, name)
			}
			sort.Strings(names)
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(names); err != nil {
					return fmt.Errorf("encoding json: %w", err)
				}
				return nil
			}
			fmt.Printf("types[%d]: %s\n", len(names), strings.Join(names, ","))
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "Output JSON array of type names")
	rootCmd.AddCommand(cmd)
}
