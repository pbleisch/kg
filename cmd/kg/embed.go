package main

import (
	"encoding/json"
	"fmt"
	"os"
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

  Batch mode (embedding.batch: true in config.yaml):
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
		Use:   "embed",
		Short: "Generate embeddings for entity attributes",
		Long: `Generate vector embeddings for entity attributes using an external command.

Use --init to scaffold a working Python embedding script and config.
Use --rebuild to force re-embedding all entities.
Use --dry-run to see what would be embedded without running the command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			initFlag, _ := cmd.Flags().GetBool("init")
			if initFlag {
				return runEmbedInit(cmd)
			}

			rebuild, _ := cmd.Flags().GetBool("rebuild")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			result, err := store.Embed(cfg.kgDir, rebuild, dryRun)
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
					fmt.Fprintf(w, "[dry-run] %d entities need embedding\n", result.Embedded)
				} else {
					fmt.Fprintf(w, "embedded: %d, skipped: %d", result.Embedded, result.Skipped)
					if result.Errors > 0 {
						fmt.Fprintf(w, ", errors: %d", result.Errors)
					}
					fmt.Fprintln(w)
				}
				for _, w := range result.Warnings {
					fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("init", false, "Scaffold embedding script and config")
	cmd.Flags().Bool("rebuild", false, "Force re-embed all entities")
	cmd.Flags().Bool("dry-run", false, "Show what would be embedded")
	rootCmd.AddCommand(cmd)
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

	// Update config.yaml to add embedding section if not present.
	configPath := filepath.Join(cfg.kgDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config.yaml: %w", err)
	}

	if !strings.Contains(string(data), "embedding:") ||
		strings.Contains(string(data), "# embedding:") {
		// Remove commented embedding block and add real one.
		content := string(data)
		// Remove any commented-out embedding lines.
		lines := strings.Split(content, "\n")
		var cleaned []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# embedding:") ||
				strings.HasPrefix(trimmed, "#   command:") ||
				strings.HasPrefix(trimmed, "#   fields:") ||
				strings.HasPrefix(trimmed, "#   batch:") ||
				trimmed == "# Run 'kg embed --init' to scaffold a working setup automatically." ||
				trimmed == "# Embeddings run automatically during 'kg sync'." {
				continue
			}
			cleaned = append(cleaned, line)
		}
		content = strings.TrimRight(strings.Join(cleaned, "\n"), "\n") + "\n"

		// Compute relative path from kgDir to script.
		relScript, _ := filepath.Rel(filepath.Dir(configPath), scriptPath)

		content += fmt.Sprintf(`
# Embedding configuration (runs automatically during kg sync)
embedding:
  command: "python3 %s"
  fields: [name, description]
  batch: true
`, relScript)

		if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("updating config.yaml: %w", err)
		}
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "Embedding setup scaffolded:")
	fmt.Fprintf(w, "  script:       %s\n", scriptPath)
	fmt.Fprintf(w, "  requirements: %s\n", reqPath)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  1. pip install -r %s\n", reqPath)
	fmt.Fprintln(w, "  2. Edit config.yaml to set your fields")
	fmt.Fprintln(w, "  3. kg embed")
	fmt.Fprintln(w, "  4. kg similar <type> --field <name> --like <entity>")
	return nil
}
