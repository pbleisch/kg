package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "merge <source> <target>",
		Short: "Merge source entity into target, rewriting all links",
		Long: `Merge combines two entities into one. Attributes from source are
copied to target (target wins on conflicts unless --prefer-source).
Outgoing links are unioned. All incoming links pointing to source
are rewritten to point to target. The source entity is then deleted.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			preferSource, _ := cmd.Flags().GetBool("prefer-source")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			crossType, _ := cmd.Flags().GetBool("cross-type")

			opts := command.MergeOptions{
				PreferSource: preferSource,
				DryRun:       dryRun,
				CrossType:    crossType,
			}

			result, err := command.Merge(cfg.kgDir, args[0], args[1], opts)
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
	cmd.Flags().Bool("prefer-source", false, "Source attributes win on conflict instead of target")
	cmd.Flags().Bool("dry-run", false, "Show what would change without writing")
	cmd.Flags().Bool("cross-type", false, "Allow merging entities of different types")
	rootCmd.AddCommand(cmd)
}
