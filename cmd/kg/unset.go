package main

import (
	"os"

	"github.com/pbleisch/kg/pkg/batch"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "unset <type:id> <attr...>",
		Short: "Remove attributes from an entity",
		Long: `Remove named attributes from an entity.

This is the only way kg removes an attribute, and it is the explicit half of
the reset rule (ADR-0030). An upsert resets only what its own contributor
wrote: items carrying another source, or no source at all, stand. So an item a
person typed by hand survives every re-ingest — and this is what removes it.

Removing an attribute the entity does not carry is not an error: unset says
what the entity must not hold, and saying it twice says it once.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := batch.UnsetAttrs(cfg.kgDir, args[0], args[1:])
			if err != nil {
				return err
			}
			f := output.NewFormatter(cfg.format)
			return f.FormatEntity(os.Stdout, e)
		},
	}
	rootCmd.AddCommand(cmd)
}
