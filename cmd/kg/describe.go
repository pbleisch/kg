package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "describe <type>",
		Short: "Describe a schema type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeName := store.ToSnakeCase(args[0])
			s, err := schema.LoadSchema(cfg.kgDir, typeName)
			if err != nil {
				return fmt.Errorf("loading schema %s: %w", typeName, err)
			}

			attrNames := make([]string, 0, len(s.Attributes))
			for name := range s.Attributes {
				attrNames = append(attrNames, name)
			}
			sort.Strings(attrNames)

			attrs := make([]output.SchemaAttr, 0, len(attrNames))
			for _, name := range attrNames {
				a := s.Attributes[name]
				attrs = append(attrs, output.SchemaAttr{
					Name:     name,
					Type:     a.Type,
					Required: a.Required,
				})
			}

			relNames := make([]string, 0, len(s.Relations))
			for name := range s.Relations {
				relNames = append(relNames, name)
			}
			sort.Strings(relNames)

			rels := make([]output.SchemaRel, 0, len(relNames))
			for _, name := range relNames {
				r := s.Relations[name]
				rels = append(rels, output.SchemaRel{
					Name:        name,
					Target:      r.Target,
					Cardinality: r.Cardinality,
				})
			}

			info := output.SchemaInfo{
				Type:        s.Type,
				Description: s.Description,
				Attributes:  attrs,
				Relations:   rels,
			}
			f := output.NewFormatter(cfg.format)
			return f.FormatSchema(os.Stdout, info)
		},
	}
	rootCmd.AddCommand(cmd)
}
