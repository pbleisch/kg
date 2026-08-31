package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/pbleisch/kg/pkg/batch"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "apply <manifest>",
		Short: "Apply a batch of typed records (supports --dry-run)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading manifest: %w", err)
			}
			env, records, err := batch.DecodeManifest(data)
			if err != nil {
				return err
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			onConflict, _ := cmd.Flags().GetString("on-conflict")
			onError, _ := cmd.Flags().GetString("on-error")
			threshold, _ := cmd.Flags().GetFloat64("dedup-threshold")
			fields, _ := cmd.Flags().GetStringSlice("dedup-fields")

			res, err := batch.Apply(cfg.kgDir, env, records, batch.Options{
				OnConflict:     batch.ConflictPolicy(onConflict),
				OnError:        batch.ErrorPolicy(onError),
				DedupFields:    fields,
				DedupThreshold: threshold,
				DryRun:         dryRun,
			})
			if err != nil {
				return err
			}
			return printApplyResult(cmd.OutOrStdout(), res)
		},
	}
	cmd.Flags().Bool("dry-run", false, "Validate and summarize the batch without writing")
	cmd.Flags().String("on-conflict", "fail", "Behavior when an identity already exists: fail, skip, merge, dedup")
	cmd.Flags().String("on-error", "fail", "Behavior when a record fails to apply: fail (abort and roll back), warn, skip")
	cmd.Flags().Float64("dedup-threshold", 0.85, "Similarity threshold for dedup mode (0.0–1.0)")
	cmd.Flags().StringSlice("dedup-fields", nil, "Fields for dedup similarity (required with --on-conflict dedup)")
	rootCmd.AddCommand(cmd)
}

// printApplyResult is the whole of this command's own behaviour: presentation.
func printApplyResult(w io.Writer, res *batch.Result) error {
	if cfg.format == output.FormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if cfg.quiet {
		return nil
	}
	fmt.Fprintf(w, "Batch version: %d\n", res.Version)
	fmt.Fprintf(w, "Idempotency key: %s\n", res.IdempotencyKey)
	if res.Replayed {
		fmt.Fprintln(w, "Already applied under this key — nothing written.")
		return nil
	}
	fmt.Fprintf(w, "Total records: %d\n", res.TotalRecords)
	for _, op := range sortedKeys(res.Counts) {
		fmt.Fprintf(w, "  %s: %d\n", op, res.Counts[op])
	}
	if res.DryRun {
		fmt.Fprintln(w, "Dry run — nothing written.")
		return nil
	}
	fmt.Fprintf(w, "Created %d, updated %d, linked %d, deleted %d, deduped %d\n",
		res.Created, res.Updated, res.Linked, res.Deleted, res.Deduped)
	for _, d := range res.Dedups {
		fmt.Fprintf(os.Stderr, "  dedup: %s → %s\n", d.From, d.To)
	}
	for _, warn := range res.Warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", warn.Message)
	}
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  error: %s\n", e.Error())
	}
	if len(res.Errors) > 0 {
		fmt.Fprintf(w, "%d record(s) failed\n", len(res.Errors))
	}
	return nil
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}
