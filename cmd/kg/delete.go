package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "delete <type:id>",
		Short: "Delete an entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := store.NormalizeEntityID(args[0])
			if err := command.Delete(cfg.kgDir, id); err != nil {
				return err
			}
			if !cfg.quiet {
				fmt.Printf("deleted %s\n", id)
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
