package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/batch"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "link <type:id> <relation> <type:id>",
		Short: "Link two entities",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := batch.LinkRef(cfg.kgDir, args[0], args[1], args[2]); err != nil {
				return err
			}
			if !cfg.quiet {
				fmt.Printf("linked %s -[%s]-> %s\n",
					store.NormalizeEntityID(args[0]),
					store.NormalizeRelation(args[1]),
					store.NormalizeEntityID(args[2]))
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
