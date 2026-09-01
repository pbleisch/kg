package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pbleisch/kg/pkg/batch"
	"github.com/pbleisch/kg/pkg/output"
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

It goes through the write boundary, so it carries an envelope, an idempotency
key and a journal: a merge that dies halfway is rolled back rather than leaving
half the referrers rewritten. It is the same path 'kg apply' takes for an
'action: merge' record.

--prefer target keeps the target's scalars instead. It applies to scalars only:
a list keeps both sides and a relation unions, so neither has a side to prefer.
The default flipped with ADR-0030 — it used to be target-wins, which made
'kg merge' and 'kg apply' give opposite answers to the same question.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prefer, _ := cmd.Flags().GetString("prefer")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			crossType, _ := cmd.Flags().GetBool("cross-type")

			// Every decision below the argv belongs to the boundary: the
			// type check, the merge rule, normalizing the two ids. This
			// command chooses nothing (P7).
			res, err := batch.MergeEntities(cfg.kgDir, args[0], args[1], prefer, !crossType, dryRun)
			if err != nil {
				return err
			}

			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			default:
				w := cmd.OutOrStdout()
				for _, m := range res.Merges {
					if dryRun {
						fmt.Fprintf(w, "[dry-run] merge %s → %s\n", m.SourceID, m.TargetID)
					} else {
						fmt.Fprintf(w, "merged %s → %s\n", m.SourceID, m.TargetID)
					}
					fmt.Fprintf(w, "  attributes updated: %d\n", m.AttrsUpdated)
					fmt.Fprintf(w, "  links added:        %d\n", m.LinksAdded)
					fmt.Fprintf(w, "  incoming rewritten: %d\n", m.IncomingRewritten)
				}
				for _, warn := range res.Warnings {
					fmt.Fprintf(os.Stderr, "  warning: %s\n", warn.Message)
				}
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
