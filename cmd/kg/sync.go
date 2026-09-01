package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "sync [entity-id...]",
		Short: "Rebuild the graph's derived data",
		Long: `Rebuild every derived artifact from the entity files.

The graph itself is the entity files; nothing is kept in sync with them, and no
read requires this command. What it builds is the vector sidecar under
.kg/.cache/ that 'kg similar' searches.

Delete .kg/.cache/ and run this: 'git status' is clean afterwards, because
everything it writes is derived (P2).

Named entity ids restrict which entities may be re-embedded; every other vector
is carried through, so the sidecar that lands is always complete.

Embedding needs a command, and kg does not choose one (P8). Set
KG_EMBED_COMMAND, or write .kg/embedder.yaml — --init scaffolds both a working
Python script and the file that names it, and --show prints what was resolved
and where from. A graph with no embedder has no derived data; say so with
--no-embed rather than letting the absence pass unremarked.

A corpus written before vectors moved out carries them as *_embedding
attributes. --migrate-vectors harvests those into the sidecar and rewrites the
files without them, so the search keeps working with no model run.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			initFlag, _ := cmd.Flags().GetBool("init")
			showFlag, _ := cmd.Flags().GetBool("show")
			rebuild, _ := cmd.Flags().GetBool("rebuild")
			noEmbed, _ := cmd.Flags().GetBool("no-embed")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			migrate, _ := cmd.Flags().GetBool("migrate-vectors")
			strip, _ := cmd.Flags().GetBool("strip-embeddings")

			if initFlag {
				return runSyncInit(cmd)
			}
			if showFlag {
				return runSyncShow(cmd)
			}
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
				Only:    args,
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
	cmd.Flags().Bool("init", false, "scaffold an embedding script and .kg/embedder.yaml")
	cmd.Flags().Bool("show", false, "print the resolved embedding command and its source")
	cmd.Flags().Bool("rebuild", false, "discard the sidecar and re-embed every field")
	cmd.Flags().Bool("no-embed", false, "skip embedding; the graph has no derived data to build")
	cmd.Flags().Bool("dry-run", false, "report what would be done without writing")
	cmd.Flags().Bool("migrate-vectors", false, "move *_embedding attributes out of the entity files into the sidecar (one-way)")
	cmd.Flags().Bool("strip-embeddings", false, "delete *_embedding attributes from the entity files without keeping the vectors")
	rootCmd.AddCommand(cmd)
}

// runSyncShow prints what kg resolved and where from. An operator who finds no
// vectors should never have to guess whether a command was configured.
func runSyncShow(cmd *cobra.Command) error {
	emb, err := store.ResolveEmbedder(cfg.kgDir)
	w := cmd.OutOrStdout()
	if err != nil {
		if cfg.format == output.FormatJSON {
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"configured": false, "error": err.Error()})
		}
		fmt.Fprintln(w, "no embedder resolved:")
		fmt.Fprintf(w, "  %v\n", err)
		return nil
	}
	if cfg.format == output.FormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"configured": true,
			"source":     emb.Source,
			"command":    emb.Command,
			"fields":     emb.Fields,
			"batch":      emb.Batch,
			"digest":     emb.Digest(),
		})
	}
	fmt.Fprintf(w, "source:  %s\n", emb.Source)
	fmt.Fprintf(w, "command: %s\n", emb.Command)
	fmt.Fprintf(w, "fields:  %s\n", strings.Join(emb.Fields, ","))
	fmt.Fprintf(w, "batch:   %v\n", emb.Batch)
	fmt.Fprintf(w, "digest:  %s\n", emb.Digest())
	return nil
}

// runSyncInit is a cover over store.ScaffoldEmbedder: it prints what the
// library wrote and what to do next, and decides nothing itself (P7).
func runSyncInit(cmd *cobra.Command) error {
	res, err := store.ScaffoldEmbedder(cfg.kgDir)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if cfg.format == output.FormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Fprintln(w, "Embedding setup scaffolded:")
	fmt.Fprintf(w, "  script:       %s\n", res.ScriptPath)
	fmt.Fprintf(w, "  requirements: %s\n", res.RequirementsPath)
	fmt.Fprintf(w, "  embedder:     %s\n", res.EmbedderPath)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  1. pip install -r %s\n", res.RequirementsPath)
	fmt.Fprintf(w, "  2. Edit %s to set your fields\n", res.EmbedderPath)
	fmt.Fprintln(w, "  3. kg sync")
	fmt.Fprintln(w, "  4. kg similar <type> --field <name> --like <entity>")
	return nil
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
