package main

import (
	"os"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

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
		// Map validation errors to a distinct exit code; otherwise treat as internal error.
		exitCode := 1
		errCode := "internal_error"
		if strings.Contains(err.Error(), "validation failed") {
			exitCode = 2
			errCode = "validation_error"
		}

		if cfg.format == output.FormatJSON {
			f := output.NewFormatter(cfg.format)
			_ = f.FormatError(os.Stderr, errCode, err.Error(), nil)
		} else {
			rootCmd.PrintErrln(err.Error())
		}
		os.Exit(exitCode)
	}
}

// sortStrings keeps presentation deterministic without pulling sort into every
// command file.
func sortStrings(ss []string) { sort.Strings(ss) }
