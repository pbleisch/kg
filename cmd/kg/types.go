package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/pbleisch/kg/pkg/output"
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

			f := output.NewFormatter(cfg.format)
			return f.FormatTypeList(os.Stdout, names)
		},
	}
	rootCmd.AddCommand(cmd)
}
