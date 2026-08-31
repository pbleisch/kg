package main

import (
	"os"

	"github.com/pbleisch/kg/pkg/batch"
	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "create <type> <localID> [attr=val...]",
		Short: "Create a new entity",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			attrs, err := command.ParseAttrs(args[2:])
			if err != nil {
				return err
			}
			upsert, _ := cmd.Flags().GetBool("upsert")

			e, err := batch.CreateEntity(cfg.kgDir, args[0], args[1], attrs, upsert)
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
