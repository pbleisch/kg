package main

import (
	"encoding/json"
	"fmt"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show graph statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			stats, err := query.Stats(cfg.kgDir)
			if err != nil {
				return err
			}

			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(stats)
			default:
				w := cmd.OutOrStdout()
				fmt.Fprintf(w, "Entities: %d\n", stats.TotalEntities)
				fmt.Fprintf(w, "Links:    %d\n", stats.TotalLinks)
				fmt.Fprintf(w, "Orphans:  %d\n", stats.Orphans)

				if len(stats.TypeCounts) > 0 {
					fmt.Fprintln(w, "\nBy type:")
					for _, kv := range query.TypeCountsSorted(stats.TypeCounts) {
						fmt.Fprintf(w, "  %-20s %d\n", kv.Key, kv.Count)
					}
				}

				if len(stats.RelCounts) > 0 {
					fmt.Fprintln(w, "\nBy relation:")
					for _, kv := range query.RelCountsSorted(stats.RelCounts) {
						fmt.Fprintf(w, "  %-20s %d\n", kv.Key, kv.Count)
					}
				}
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
