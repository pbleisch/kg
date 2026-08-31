package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/batch"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "unlink <type:id> <relation> <type:id>",
		Short: "Unlink two entities",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := store.NormalizeEntityID(args[0])
			rel := store.NormalizeRelation(args[1])
			tgt := store.NormalizeEntityID(args[2])
			if err := batch.UnlinkRef(cfg.kgDir, src, rel, tgt); err != nil {
				return err
			}
			if !cfg.quiet {
				fmt.Printf("unlinked %s -[%s]-> %s\n", src, rel, tgt)
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
