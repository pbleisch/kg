package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

const embedScript = `#!/usr/bin/env python3
"""Default embedding script for kg.

Uses sentence-transformers with the all-MiniLM-L6-v2 model (384 dimensions).
Runs locally on CPU — no API key needed. First run downloads ~80MB model.

Protocol:
  Single mode (default):
    stdin:  {"text": "...", "field": "name", "entity_id": "person:alice", "type": "person"}
    stdout: [0.1, 0.2, ...]

  Batch mode (batch: true in .kg/embedder.yaml):
    stdin:  {"items": [{"text": "...", "field": "...", "entity_id": "...", "type": "..."}, ...]}
    stdout: {"vectors": [[0.1, ...], [0.2, ...], ...]}
"""

import json
import sys

from sentence_transformers import SentenceTransformer

MODEL_NAME = "all-MiniLM-L6-v2"

def get_model():
    return SentenceTransformer(MODEL_NAME)

def main():
    data = json.load(sys.stdin)

    model = get_model()

    # Batch mode
    if "items" in data:
        texts = [item["text"] for item in data["items"]]
        vectors = model.encode(texts).tolist()
        json.dump({"vectors": vectors}, sys.stdout)
    else:
        # Single mode
        text = data["text"]
        vector = model.encode(text).tolist()
        json.dump(vector, sys.stdout)

if __name__ == "__main__":
    main()
`

const embedRequirements = "sentence-transformers>=2.2.0\n"

func init() {
	cmd := &cobra.Command{
		Use:   "embed [entity-id...]",
		Short: "Generate embeddings for entity attributes",
		Long: `Generate vector embeddings for entity attributes using an external command.

kg never chooses, hosts or calls a model (P8): the embedder is a program you
configure, talking JSON over stdin and stdout. Point kg at one with
KG_EMBED_COMMAND, or with .kg/embedder.yaml — --init scaffolds both a working
Python script and the file that names it.

Vectors are written to the gitignored sidecar under .kg/.cache/, never into the
entity files.

Named entity ids restrict which entities may be re-embedded; every other vector
is carried through, so the sidecar that lands is always complete.

Use --show to print the resolved command and where it came from.
Use --rebuild to force re-embedding everything.
Use --dry-run to see what would be embedded without running the command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			initFlag, _ := cmd.Flags().GetBool("init")
			if initFlag {
				return runEmbedInit(cmd)
			}

			showFlag, _ := cmd.Flags().GetBool("show")
			if showFlag {
				return runEmbedShow(cmd)
			}

			rebuild, _ := cmd.Flags().GetBool("rebuild")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			result, err := store.Embed(cfg.kgDir, rebuild, dryRun, args...)
			if err != nil {
				return err
			}

			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			default:
				w := cmd.OutOrStdout()
				if dryRun {
					fmt.Fprintf(w, "[dry-run] %d vector(s) need embedding, %d unchanged\n",
						result.Embedded, result.Carried)
				} else {
					fmt.Fprintf(w, "embedded: %d, carried: %d, dropped: %d",
						result.Embedded, result.Carried, result.Dropped)
					if result.Errors > 0 {
						fmt.Fprintf(w, ", errors: %d", result.Errors)
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
	cmd.Flags().Bool("init", false, "Scaffold an embedding script and .kg/embedder.yaml")
	cmd.Flags().Bool("show", false, "Print the resolved embedding command and its source")
	cmd.Flags().Bool("rebuild", false, "Force re-embed everything")
	cmd.Flags().Bool("dry-run", false, "Show what would be embedded")
	rootCmd.AddCommand(cmd)
}

// runEmbedShow prints what kg resolved and where from. An operator who finds
// no vectors should never have to guess whether a command was configured.
func runEmbedShow(cmd *cobra.Command) error {
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

func runEmbedInit(cmd *cobra.Command) error {
	scriptsDir := filepath.Join(cfg.kgDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		return fmt.Errorf("creating scripts dir: %w", err)
	}

	scriptPath := filepath.Join(scriptsDir, "embed.py")
	if _, err := os.Stat(scriptPath); err == nil {
		return fmt.Errorf("%s already exists; remove it first to re-scaffold", scriptPath)
	}
	if err := os.WriteFile(scriptPath, []byte(embedScript), 0o755); err != nil {
		return fmt.Errorf("writing embed.py: %w", err)
	}

	reqPath := filepath.Join(scriptsDir, "requirements.txt")
	if err := os.WriteFile(reqPath, []byte(embedRequirements), 0o644); err != nil {
		return fmt.Errorf("writing requirements.txt: %w", err)
	}

	// The command is written absolute, and to a file that does not travel with
	// the graph. A relative command is the bug ADR-0025 is about: it resolves
	// against whatever directory kg happens to be run from, so `kg sync` from
	// one place embeds and from another silently does not.
	absScript, err := filepath.Abs(scriptPath)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", scriptPath, err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("python3 is not on PATH; write %s by hand with an absolute interpreter path", store.EmbedderPath(cfg.kgDir))
	}

	embedderPath := store.EmbedderPath(cfg.kgDir)
	if _, err := os.Stat(embedderPath); err == nil {
		return fmt.Errorf("%s already exists; remove it first to re-scaffold", embedderPath)
	}
	embedderContent := fmt.Sprintf(`# Which embedder kg runs, for THIS checkout.
#
# Not part of the graph: it names a program on this machine, so it must not be
# committed. Add %s to the graph repository's .gitignore.
# A pipeline can override it with KG_EMBED_COMMAND instead.
#
# Paths must be absolute, or a bare program name on PATH.
command: "%s %s"
fields: [name, description]
batch: true
`, store.EmbedderName, python, absScript)
	if err := os.WriteFile(embedderPath, []byte(embedderContent), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", embedderPath, err)
	}

	if err := ensureGraphGitignore(cfg.kgDir); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "Embedding setup scaffolded:")
	fmt.Fprintf(w, "  script:       %s\n", scriptPath)
	fmt.Fprintf(w, "  requirements: %s\n", reqPath)
	fmt.Fprintf(w, "  embedder:     %s\n", embedderPath)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  1. pip install -r %s\n", reqPath)
	fmt.Fprintf(w, "  2. Edit %s to set your fields\n", embedderPath)
	fmt.Fprintln(w, "  3. kg sync")
	fmt.Fprintln(w, "  4. kg similar <type> --field <name> --like <entity>")
	return nil
}

// ensureGraphGitignore keeps the derived and per-checkout files out of the
// graph repository. Both are things a clone regenerates; neither is knowledge.
func ensureGraphGitignore(kgDir string) error {
	path := filepath.Join(kgDir, ".gitignore")
	want := []string{".cache/", store.EmbedderName}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	lines := strings.Split(string(existing), "\n")
	have := make(map[string]bool, len(lines))
	for _, l := range lines {
		have[strings.TrimSpace(l)] = true
	}

	out := strings.TrimRight(string(existing), "\n")
	added := false
	for _, w := range want {
		if have[w] {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += w
		added = true
	}
	if !added {
		return nil
	}
	if !strings.HasPrefix(out, "#") {
		out = "# Derived and per-checkout; rebuild with 'kg sync'\n" + out
	}
	return os.WriteFile(path, []byte(out+"\n"), 0o644)
}
