package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "merge <source> <target>",
		Short: "Merge source entity into target, rewriting all links",
		Long: `Merge combines two entities into one, by kg's one merge rule.

A scalar attribute takes the incoming (source) value, and overwriting a
different one is reported. An attribute the schema declares 'type: list' keeps
both accounts: the two item sets concatenate, deduplicated on (source, text),
because two entities being merged genuinely were written by different sources.
Outgoing links are unioned. All incoming links pointing at source are rewritten
to point at target, and the source entity is then deleted.

--prefer target keeps the target's scalars instead. It applies to scalars only:
a list keeps both sides and a relation unions, so neither has a side to prefer.
The default flipped with ADR-0030 — it used to be target-wins, which made
'kg merge' and 'kg apply' give opposite answers to the same question.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prefer, _ := cmd.Flags().GetString("prefer")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			crossType, _ := cmd.Flags().GetBool("cross-type")

			opts := command.MergeOptions{
				Prefer:    prefer,
				DryRun:    dryRun,
				CrossType: crossType,
			}

			// A merge rewrites every inbound link to the source, so it needs
			// the whole graph. Load it here so a failure to read the corpus
			// surfaces before anything is written.
			g, err := store.LoadGraph(cfg.kgDir)
			if err != nil {
				return fmt.Errorf("loading graph: %w", err)
			}
			opts.Graph = g

			result, err := command.Merge(cfg.kgDir, store.NormalizeEntityID(args[0]), store.NormalizeEntityID(args[1]), opts)
			if err != nil {
				return err
			}

			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			default:
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] merge %s → %s\n", result.SourceID, result.TargetID)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "merged %s → %s\n", result.SourceID, result.TargetID)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  attributes updated: %d\n", result.AttrsUpdated)
				fmt.Fprintf(cmd.OutOrStdout(), "  links added:        %d\n", result.LinksAdded)
				fmt.Fprintf(cmd.OutOrStdout(), "  incoming rewritten: %d\n", result.IncomingRewritten)
				if dryRun {
					fmt.Fprintln(os.Stderr, "no changes written (dry-run)")
				}
			}
			return nil
		},
	}
	cmd.Flags().String("prefer", "source", "Which side wins a scalar conflict: source (incoming) or target")
	cmd.Flags().Bool("dry-run", false, "Show what would change without writing")
	cmd.Flags().Bool("cross-type", false, "Allow merging entities of different types")
	rootCmd.AddCommand(cmd)
}
