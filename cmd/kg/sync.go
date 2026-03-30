package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync YAML files to the index",
		RunE: func(cmd *cobra.Command, args []string) error {
			rebuild, _ := cmd.Flags().GetBool("rebuild")

			var result *store.SyncResult
			var err error

			if rebuild {
				result, err = store.RebuildWithResult(cfg.kgDir)
			} else {
				result, err = store.SyncWithResult(cfg.kgDir)
			}
			if err != nil {
				return err
			}

			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			default:
				action := "sync"
				if rebuild {
					action = "rebuild"
				}
				if result.Skipped > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "%s complete: %d synced, %d skipped\n",
						action, result.Synced, result.Skipped)
					for _, w := range result.Warnings {
						fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
					}
				} else if !cfg.quiet {
					fmt.Fprintf(cmd.OutOrStdout(), "%s complete\n", action)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("rebuild", false, "full rebuild instead of incremental sync")
	rootCmd.AddCommand(cmd)
}
