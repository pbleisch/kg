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
		Use:   "set <type:id> <attr=val...>",
		Short: "Set attributes on an entity",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			attrs, err := command.ParseAttrs(args[1:])
			if err != nil {
				return err
			}
			e, err := command.Set(cfg.kgDir, store.NormalizeEntityID(args[0]), attrs)
			if err != nil {
				return err
			}
			f := output.NewFormatter(cfg.format)
			return f.FormatEntity(os.Stdout, e)
		},
	}
	rootCmd.AddCommand(cmd)
}
