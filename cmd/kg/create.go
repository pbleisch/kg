package main

import (
	"os"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "create <Type> <localID> [attr=val...]",
		Short: "Create a new entity",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			attrs, err := command.ParseAttrs(args[2:])
			if err != nil {
				return err
			}
			e, err := command.Create(cfg.kgDir, args[0], args[1], attrs)
			if err != nil {
				return err
			}
			f := output.NewFormatter(cfg.format)
			return f.FormatEntity(os.Stdout, e)
		},
	}
	rootCmd.AddCommand(cmd)
}
