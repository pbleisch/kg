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
		Long: `Apply a manifest of typed records through kg's one write boundary.

Entity resolution is not part of a write. It used to be, as --on-conflict
dedup, running once per batch against a vector sidecar that was always one
document behind; it is now a separate whole-corpus pass — see ` + "`kg resolve`" + `.`,
		Args: cobra.MinimumNArgs(1),
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

			res, err := batch.Apply(cfg.kgDir, env, records, batch.Options{
				OnConflict: batch.ConflictPolicy(onConflict),
				OnError:    batch.ErrorPolicy(onError),
				DryRun:     dryRun,
			})
			if err != nil {
				return err
			}
			return printApplyResult(cmd.OutOrStdout(), res)
		},
	}
	cmd.Flags().Bool("dry-run", false, "Validate and summarize the batch without writing")
	cmd.Flags().String("on-conflict", "fail", "Behavior when an identity already exists: fail, skip, merge")
	cmd.Flags().String("on-error", "fail", "Behavior when a record fails to apply: fail (abort and roll back), warn, skip")
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
	fmt.Fprintf(w, "Created %d, updated %d, linked %d, deleted %d\n",
		res.Created, res.Updated, res.Linked, res.Deleted)
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
