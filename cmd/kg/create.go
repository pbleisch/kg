package main

import (
	"os"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
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

			upsert, _ := cmd.Flags().GetBool("upsert")

			var e *store.Entity
			if upsert {
				e, err = command.Upsert(cfg.kgDir, args[0], args[1], attrs)
			} else {
				e, err = command.Create(cfg.kgDir, args[0], args[1], attrs)
			}
			if err != nil {
				return err
			}
			f := output.NewFormatter(cfg.format)
			return f.FormatEntity(os.Stdout, e)
		},
	}
	cmd.Flags().Bool("upsert", false, "Create or update if entity already exists")
	rootCmd.AddCommand(cmd)
}
