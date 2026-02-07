package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync YAML files to the index",
		RunE: func(cmd *cobra.Command, args []string) error {
			rebuild, _ := cmd.Flags().GetBool("rebuild")
			if rebuild {
				if err := store.Rebuild(cfg.kgDir); err != nil {
					return err
				}
				if !cfg.quiet {
					fmt.Println("rebuild complete")
				}
				return nil
			}
			if err := store.Sync(cfg.kgDir); err != nil {
				return err
			}
			if !cfg.quiet {
				fmt.Println("sync complete")
			}
			return nil
		},
	}
	cmd.Flags().Bool("rebuild", false, "full rebuild instead of incremental sync")
	rootCmd.AddCommand(cmd)
}
