package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Rebuild the graph's derived data",
		Long: `Rebuild every derived artifact from the entity files.

The graph itself is the entity files; nothing is kept in sync with them, and no
read requires this command. What it builds is the vector sidecar under
.kg/.cache/ that 'kg similar' searches.

Delete .kg/.cache/ and run this: 'git status' is clean afterwards, because
everything it writes is derived (P2).

A corpus written before vectors moved out carries them as *_embedding
attributes. --migrate-vectors harvests those into the sidecar and rewrites the
files without them, so the search keeps working with no model run.

Embedding needs a command, and kg does not choose one (P8). Set
KG_EMBED_COMMAND, or write .kg/embedder.yaml — 'kg embed --init' scaffolds one.
A graph with no embedder has no derived data; say so with --no-embed rather
than letting the absence pass unremarked.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rebuild, _ := cmd.Flags().GetBool("rebuild")
			noEmbed, _ := cmd.Flags().GetBool("no-embed")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			migrate, _ := cmd.Flags().GetBool("migrate-vectors")
			strip, _ := cmd.Flags().GetBool("strip-embeddings")
			if migrate && strip {
				return fmt.Errorf("--migrate-vectors and --strip-embeddings are alternatives: the first keeps the vectors, the second discards them")
			}
			if migrate {
				return runMigrateVectors(cmd, dryRun)
			}
			if strip {
				return runStripEmbeddings(cmd, dryRun)
			}

			result, err := store.Sync(cfg.kgDir, store.SyncOptions{
				Rebuild: rebuild,
				NoEmbed: noEmbed,
				DryRun:  dryRun,
			})
			if err != nil {
				var missing *store.ErrNoEmbedder
				if errors.As(err, &missing) {
					return fmt.Errorf("%w\n\nA graph with no embedder has no derived data to build; pass --no-embed to say so deliberately", err)
				}
				return err
			}

			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			default:
				w := cmd.OutOrStdout()
				prefix := ""
				if dryRun {
					prefix = "[dry-run] "
				}
				fmt.Fprintf(w, "%s%d entities", prefix, result.Entities)
				if noEmbed {
					fmt.Fprintln(w, "; embedding skipped")
				} else {
					fmt.Fprintf(w, "; vectors: %d embedded, %d carried, %d dropped, %d rows",
						result.Embedded, result.Carried, result.Dropped, result.Rows)
					if result.Errors > 0 {
						fmt.Fprintf(w, ", %d errors", result.Errors)
					}
					fmt.Fprintln(w)
				}
				for _, warn := range result.Warnings {
					fmt.Fprintf(os.Stderr, "  warning: %s\n", warn)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("rebuild", false, "discard the sidecar and re-embed every field")
	cmd.Flags().Bool("no-embed", false, "skip embedding; the graph has no derived data to build")
	cmd.Flags().Bool("dry-run", false, "report what would be done without writing")
	cmd.Flags().Bool("migrate-vectors", false, "move *_embedding attributes out of the entity files into the sidecar (one-way)")
	cmd.Flags().Bool("strip-embeddings", false, "delete *_embedding attributes from the entity files without keeping the vectors")
	rootCmd.AddCommand(cmd)
}

// runStripEmbeddings is the migration off the arrangement where vectors lived
// in the entity YAML. It writes to the source of truth, so it is never
// something a read or an ordinary sync does on the operator's behalf.
func runStripEmbeddings(cmd *cobra.Command, dryRun bool) error {
	changed, freed, err := store.StripLegacyEmbeddings(cfg.kgDir, dryRun)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if cfg.format == output.FormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"changed":    changed,
			"bytes":      freed,
			"dry_run":    dryRun,
			"entity_dir": cfg.kgDir,
		})
	}
	verb := "rewrote"
	if dryRun {
		verb = "[dry-run] would rewrite"
	}
	fmt.Fprintf(w, "%s %d entity file(s), freeing %.1f MB\n", verb, changed, float64(freed)/1e6)
	if changed > 0 && !dryRun {
		fmt.Fprintln(w, "run 'kg sync' to build the vector sidecar")
	}
	return nil
}

// runMigrateVectors moves the vectors out of the entity files and into the
// sidecar. It writes to the source of truth, so it is a thing the operator
// asks for once, not something a sync does on their behalf.
func runMigrateVectors(cmd *cobra.Command, dryRun bool) error {
	result, err := store.MigrateLegacyVectors(cfg.kgDir, dryRun)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if cfg.format == output.FormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	prefix := ""
	if dryRun {
		prefix = "[dry-run] "
	}
	fmt.Fprintf(w, "%sharvested %d vector(s) of %d dimensions from %d entity file(s), freeing %.1f MB\n",
		prefix, result.Harvested, result.Dim, result.Files, float64(result.Bytes)/1e6)
	for _, warn := range result.Warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", warn)
	}
	if !dryRun && result.Harvested > 0 {
		fmt.Fprintln(w, "the vectors now live in .kg/.cache/; commit the entity files and gitignore .kg/.cache/")
	}
	return nil
}
