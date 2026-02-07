package main

import (
	"fmt"
	"sort"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "describe <Type>",
		Short: "Describe a schema type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeName := args[0]
			s, err := schema.LoadSchema(cfg.kgDir, typeName)
			if err != nil {
				return fmt.Errorf("loading schema %s: %w", typeName, err)
			}
			fmt.Printf("type: %s\n", s.Type)
			if s.Description != "" {
				fmt.Printf("description: %s\n", s.Description)
			}

			if len(s.Attributes) > 0 {
				fmt.Println("attributes:")
				attrNames := make([]string, 0, len(s.Attributes))
				for name := range s.Attributes {
					attrNames = append(attrNames, name)
				}
				sort.Strings(attrNames)
				for _, name := range attrNames {
					a := s.Attributes[name]
					req := ""
					if a.Required {
						req = " (required)"
					}
					fmt.Printf("  %s: %s%s\n", name, a.Type, req)
				}
			}

			if len(s.Relations) > 0 {
				fmt.Println("relations:")
				relNames := make([]string, 0, len(s.Relations))
				for name := range s.Relations {
					relNames = append(relNames, name)
				}
				sort.Strings(relNames)
				for _, name := range relNames {
					r := s.Relations[name]
					fmt.Printf("  %s: -> %s [%s]\n", name, r.Target, r.Cardinality)
				}
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
