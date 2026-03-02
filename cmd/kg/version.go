package main

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the kg version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			v := version
			if store.LadybugAvailable() {
				v += " (ladybug)"
			}
			fmt.Println(v)
		},
	}
	rootCmd.AddCommand(cmd)
}
