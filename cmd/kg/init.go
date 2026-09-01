package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new knowledge graph directory",
		Long: `Create .kg/ with entities/, schema/, .cache/, a config.yaml, and the
.gitignore entries that keep derived and per-checkout files out of the graph
repository.

The layout is store.Init's, not this command's: a Go consumer that links kg
scaffolds a graph with the same call (P7).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := store.Init(cfg.kgDir)
			if err != nil {
				return err
			}
			if !cfg.quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "Initialized knowledge graph in %s\n", res.Dir)
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
