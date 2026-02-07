package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "unlink <type:id> <relation> <type:id>",
		Short: "Unlink two entities",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := command.Unlink(cfg.kgDir, args[0], args[1], args[2]); err != nil {
				return err
			}
			if !cfg.quiet {
				fmt.Printf("unlinked %s -[%s]-> %s\n", args[0], args[1], args[2])
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
