package main

import (
	"os"
	"sort"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
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
	// main() is the only thing that prints an error, so that there is one
	// error shape and --json gets a structured one rather than a structured
	// one preceded by a line of prose (P6).
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Past argument parsing, a failure is a result and not a misuse, so
		// stop printing the usage block over it. A graph that refuses to load
		// names 2,550 files; burying that under a flag list helps nobody.
		cmd.SilenceUsage = true

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
		// A graph that breaks P3's enforced half gets its own exit code, so a
		// caller can tell "this graph does not load" from "kg fell over"
		// without reading the message (ADR-0018, ADR-0028).
		exitCode := 1
		errCode := "internal_error"
		var suggestions []string
		if errs, refused := store.LoadRefused(err); refused {
			exitCode = 2
			errCode = "graph_refused"
			suggestions = repairHint(errs)
		}

		if cfg.format == output.FormatJSON {
			f := output.NewFormatter(cfg.format)
			_ = f.FormatError(os.Stderr, errCode, err.Error(), suggestions)
		} else {
			rootCmd.PrintErrln(err.Error())
			for _, s := range suggestions {
				rootCmd.PrintErrln(s)
			}
		}
		os.Exit(exitCode)
	}
}

// repairHint names the command that fixes what refused, for the classes a
// command can fix. Presentation, not behaviour: the library states the
// problem and the CLI says which of its own verbs addresses it (P7).
func repairHint(errs []store.Structural) []string {
	repairable, needsAChoice := false, false
	for _, e := range errs {
		switch e.Code {
		case "id_disagreement", "type_disagreement", "illegal_attribute":
			repairable = true
		case "identity_collision", "non_canonical_path", "bad_path":
			needsAChoice = true
		}
	}
	var out []string
	if repairable {
		out = append(out, "run `kg validate --fix` to rewrite each file to agree with its path")
	}
	if needsAChoice {
		out = append(out, "run `kg validate` for the files that need a decision kg will not make")
	}
	if len(out) == 0 {
		out = append(out, "run `kg validate` to see every finding")
	}
	return out
}

// sortStrings keeps presentation deterministic without pulling sort into every
// command file.
func sortStrings(ss []string) { sort.Strings(ss) }
