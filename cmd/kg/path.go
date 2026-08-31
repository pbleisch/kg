package main

import (
	"fmt"
	"os"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "path <id> [-> <rel>]...",
		Short: "Traverse forward relations from an entity",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			startID := store.NormalizeEntityID(args[0])

			var steps []query.PathStep
			i := 1
			for i < len(args) {
				arrow := args[i]
				recursive := false
				switch arrow {
				case "->":
				// single hop
				case "->*":
					recursive = true
				default:
					return fmt.Errorf("expected '->' or '->*', got %q", arrow)
				}
				i++
				if i >= len(args) {
					return fmt.Errorf("missing relation name after %s", arrow)
				}
				steps = append(steps, query.PathStep{
					Relation:  store.NormalizeRelation(args[i]),
					Recursive: recursive,
				})
				i++
			}

			f := output.NewFormatter(cfg.format)
			verbose, _ := cmd.Flags().GetBool("verbose")

			if verbose {
				results, err := query.PathVerbose(cfg.kgDir, startID, steps)
				if err != nil {
					return err
				}
				out := make([]output.PathResult, len(results))
				for j, r := range results {
					out[j] = output.PathResult{ID: r.ID, Depth: r.Depth, Via: r.Via}
				}
				return f.FormatPathVerbose(os.Stdout, out)
			}

			ids, err := query.Path(cfg.kgDir, startID, steps)
			if err != nil {
				return err
			}
			return f.FormatPathIDs(os.Stdout, ids)
		},
	}
	cmd.Flags().Bool("verbose", false, "show traversal depth and relation info")
	cmd.Flags().SetInterspersed(false)
	rootCmd.AddCommand(cmd)
}
