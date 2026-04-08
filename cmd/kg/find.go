package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	var limit, offset int
	var includeEmbeddings bool

	cmd := &cobra.Command{
		Use:   "find <type> [where <filters...>]",
		Short: "Find entities by type with optional filters",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeName := store.ToSnakeCase(args[0])

			var filters []query.Filter
			rest := args[1:]
			if len(rest) > 0 {
				if strings.EqualFold(rest[0], "where") {
					rest = rest[1:]
				}
				raw := strings.Join(rest, " ")
				for _, expr := range strings.Split(raw, ",") {
					expr = strings.TrimSpace(expr)
					if expr == "" {
						continue
					}
					f, err := query.ParseFilter(expr)
					if err != nil {
						return fmt.Errorf("bad filter: %w", err)
					}
					filters = append(filters, f)
				}
			}

			opts := store.QueryOptions{
				Filters:           filters,
				Limit:             limit,
				Offset:            offset,
				IncludeEmbeddings: includeEmbeddings,
			}

			entities, err := query.FindWithOpts(cfg.kgDir, typeName, opts)
			if err != nil {
				return err
			}
			f := output.NewFormatter(cfg.format)
			return f.FormatEntities(os.Stdout, entities)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of results (0 = no limit)")
	cmd.Flags().IntVar(&offset, "offset", 0, "skip this many results")
	cmd.Flags().BoolVar(&includeEmbeddings, "include-embeddings", false, "include embedding vectors in output")

	rootCmd.AddCommand(cmd)
}
