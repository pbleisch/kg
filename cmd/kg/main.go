package main

import (
	"os"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

// cfg holds shared CLI configuration for all subcommands.
var cfg struct {
	kgDir  string
	format output.Format
	quiet  bool
}

var rootCmd = &cobra.Command{
	Use:   "kg",
	Short: "Knowledge graph maintenance CLI",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		jsonFlag, _ := cmd.Flags().GetBool("json")
		humanFlag, _ := cmd.Flags().GetBool("human")
		switch {
		case jsonFlag:
			cfg.format = output.FormatJSON
		case humanFlag:
			cfg.format = output.FormatHuman
		default:
			cfg.format = output.FormatTOON
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfg.kgDir, "dir", ".kg", "path to knowledge graph directory")
	rootCmd.PersistentFlags().Bool("json", false, "output in JSON format")
	rootCmd.PersistentFlags().Bool("human", false, "output in human-readable format")
	rootCmd.PersistentFlags().BoolVar(&cfg.quiet, "quiet", false, "suppress non-error output")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
