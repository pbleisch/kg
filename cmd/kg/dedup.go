package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "dedup <type>",
		Short: "Find and merge duplicate entities by vector similarity",
		Long: `Find duplicate entities of a given type using vector similarity
on embedding fields. By default shows duplicates (dry-run). Use --apply to merge.

Examples:
  kg dedup concept --threshold 0.85 --fields name,description
  kg dedup concept --threshold 0.85 --fields name --apply
  kg dedup concept --threshold 0.90 --fields name --apply --prefer-source`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeName := args[0]
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			fieldsStr, _ := cmd.Flags().GetString("fields")
			apply, _ := cmd.Flags().GetBool("apply")
			preferSource, _ := cmd.Flags().GetBool("prefer-source")
			crossType, _ := cmd.Flags().GetBool("cross-type")

			if fieldsStr == "" {
				return fmt.Errorf("--fields is required")
			}
			if threshold <= 0 || threshold > 1 {
				return fmt.Errorf("--threshold must be between 0 and 1")
			}

			fields := strings.Split(fieldsStr, ",")
			for i := range fields {
				fields[i] = strings.TrimSpace(fields[i])
			}

			opts := command.DedupOptions{
				Threshold:    threshold,
				Fields:       fields,
				DryRun:       !apply,
				PreferSource: preferSource,
				CrossType:    crossType,
			}

			result, err := command.FindDuplicates(cfg.kgDir, typeName, opts)
			if err != nil {
				return err
			}

			if apply && result.ClustersFound > 0 {
				if err := command.ApplyDedup(cfg.kgDir, result, opts); err != nil {
					return err
				}
			}

			w := cmd.OutOrStdout()
			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			default:
				if result.ClustersFound == 0 {
					if !cfg.quiet {
						fmt.Fprintf(w, "no duplicates found across %d entities\n", result.TotalEntities)
					}
					return nil
				}

				for _, cluster := range result.Clusters {
					for _, dup := range cluster.Duplicates {
						fmt.Fprintf(w, "%s → %s (%.2f, %s)\n",
							dup.ID, cluster.CanonicalID, dup.Score, dup.Field)
					}
				}
				if apply {
					fmt.Fprintf(w, "%d duplicates merged into %d clusters across %d entities\n",
						result.Merged, result.ClustersFound, result.TotalEntities)
				} else {
					fmt.Fprintf(w, "%d duplicates found, %d clusters across %d entities\n",
						result.DuplicatesFound, result.ClustersFound, result.TotalEntities)
				}
				for _, w := range result.Warnings {
					fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
				}
			}
			return nil
		},
	}
	cmd.Flags().Float64("threshold", 0.85, "Similarity threshold (0.0–1.0)")
	cmd.Flags().String("fields", "", "Comma-separated embedding fields to compare (required)")
	cmd.Flags().Bool("apply", false, "Actually merge duplicates (default is dry-run)")
	cmd.Flags().Bool("prefer-source", false, "Source attributes win on merge conflicts")
	cmd.Flags().Bool("cross-type", false, "Compare across entity types")
	rootCmd.AddCommand(cmd)
}
