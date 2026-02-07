package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "delete <type:id>",
		Short: "Delete an entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := command.Delete(cfg.kgDir, args[0]); err != nil {
				return err
			}
			if !cfg.quiet {
				fmt.Printf("deleted %s\n", args[0])
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
