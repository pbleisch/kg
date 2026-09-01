package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Scaffolding a graph is a library call, not a thing the CLI knows how to do.
//
// Every byte of it used to live in `cmd/kg/init.go` and `cmd/kg/embed.go`,
// which meant a Go consumer that links kg could read and write a graph but
// could not create one. That is P7's completeness half failing in the
// CLI→library direction — "a library that can do things the CLI cannot" read
// backwards — and PRINCIPLES.md names that direction as the way the guarantee
// is most likely to be lost. `kg init` and `kg sync --init` are now covers
// over these two calls and hold no behaviour of their own.

// configTemplate is what `Init` writes. It travels with the graph, so it holds
// nothing machine-specific: naming an embedding command here is the mistake
// ADR-0025 refuses.
const configTemplate = `# kg knowledge graph configuration
# Entity YAML files are stored in entities/<type>/<id>.yaml
# Schema files are stored in schema/<type>.yaml
#
# This file travels with the graph, so it holds nothing machine-specific.
# The embedding command is NOT configured here: it names a program on one
# machine. Set KG_EMBED_COMMAND, or run 'kg sync --init' to scaffold
# embedder.yaml beside this file (gitignored, per checkout). See ADR-0025.
`

const cacheGitignore = "# Derived data — rebuild with 'kg sync'\n*\n!.gitignore\n"

// InitResult names what Init created, so a caller can report it without
// re-deriving the layout.
type InitResult struct {
	Dir        string   `json:"dir"`
	ConfigPath string   `json:"config_path"`
	Created    []string `json:"created"`
}

// Init scaffolds a new graph directory: entities/, schema/, .cache/, a
// config.yaml, and the two .gitignore entries that keep derived and
// per-checkout files out of the graph repository.
//
// It refuses a directory that already holds a config.yaml rather than
// overwriting one — the file is small, but it is the only thing in .kg/ that
// is neither derived nor an entity.
func Init(kgDir string) (*InitResult, error) {
	configPath := filepath.Join(kgDir, "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return nil, fmt.Errorf("directory %s already contains a config.yaml; aborting to prevent overwrite", kgDir)
	}

	res := &InitResult{Dir: kgDir, ConfigPath: configPath}
	for _, d := range []string{
		filepath.Join(kgDir, "entities"),
		filepath.Join(kgDir, "schema"),
		CacheDir(kgDir),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", d, err)
		}
		res.Created = append(res.Created, d)
	}

	if err := os.WriteFile(configPath, []byte(configTemplate), 0o644); err != nil {
		return nil, fmt.Errorf("writing config.yaml: %w", err)
	}
	res.Created = append(res.Created, configPath)

	if err := EnsureGraphGitignore(kgDir); err != nil {
		return nil, err
	}

	cacheIgnore := filepath.Join(CacheDir(kgDir), ".gitignore")
	if err := os.WriteFile(cacheIgnore, []byte(cacheGitignore), 0o644); err != nil {
		return nil, fmt.Errorf("writing .cache/.gitignore: %w", err)
	}
	res.Created = append(res.Created, cacheIgnore)
	return res, nil
}

// EmbedScript is the embedder `ScaffoldEmbedder` writes: sentence-transformers
// over stdin/stdout. kg neither hosts nor calls a model (P8); this is a
// starting point an operator edits, not a default kg falls back to.
const EmbedScript = `#!/usr/bin/env python3
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

// EmbedRequirements is what the scaffolded script needs installed.
const EmbedRequirements = "sentence-transformers>=2.2.0\n"

// ScaffoldResult names the three files ScaffoldEmbedder wrote.
type ScaffoldResult struct {
	ScriptPath       string `json:"script_path"`
	RequirementsPath string `json:"requirements_path"`
	EmbedderPath     string `json:"embedder_path"`
}

// ScaffoldEmbedder writes a working embedding script and the per-checkout file
// that names it.
//
// The command it writes is absolute. A relative one is the bug ADR-0025 is
// about: it resolves against whatever directory kg happens to be run from, so
// a sync from one place embeds and from another silently does not.
//
// It refuses to overwrite either file, because the script is the one thing
// under .kg/ an operator is expected to edit.
func ScaffoldEmbedder(kgDir string) (*ScaffoldResult, error) {
	scriptsDir := filepath.Join(kgDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating scripts dir: %w", err)
	}

	scriptPath := filepath.Join(scriptsDir, "embed.py")
	if _, err := os.Stat(scriptPath); err == nil {
		return nil, fmt.Errorf("%s already exists; remove it first to re-scaffold", scriptPath)
	}

	embedderPath := EmbedderPath(kgDir)
	if _, err := os.Stat(embedderPath); err == nil {
		return nil, fmt.Errorf("%s already exists; remove it first to re-scaffold", embedderPath)
	}

	absScript, err := filepath.Abs(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", scriptPath, err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return nil, fmt.Errorf("python3 is not on PATH; write %s by hand with an absolute interpreter path", embedderPath)
	}

	if err := os.WriteFile(scriptPath, []byte(EmbedScript), 0o755); err != nil {
		return nil, fmt.Errorf("writing embed.py: %w", err)
	}
	reqPath := filepath.Join(scriptsDir, "requirements.txt")
	if err := os.WriteFile(reqPath, []byte(EmbedRequirements), 0o644); err != nil {
		return nil, fmt.Errorf("writing requirements.txt: %w", err)
	}

	content := fmt.Sprintf(`# Which embedder kg runs, for THIS checkout.
#
# Not part of the graph: it names a program on this machine, so it must not be
# committed. Add %s to the graph repository's .gitignore.
# A pipeline can override it with KG_EMBED_COMMAND instead.
#
# Paths must be absolute, or a bare program name on PATH.
command: "%s %s"
fields: [name, description]
batch: true
`, EmbedderName, python, absScript)
	if err := os.WriteFile(embedderPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", embedderPath, err)
	}

	if err := EnsureGraphGitignore(kgDir); err != nil {
		return nil, err
	}
	return &ScaffoldResult{
		ScriptPath:       scriptPath,
		RequirementsPath: reqPath,
		EmbedderPath:     embedderPath,
	}, nil
}

// EnsureGraphGitignore keeps the derived and per-checkout files out of the
// graph repository. Both are things a clone regenerates; neither is knowledge.
func EnsureGraphGitignore(kgDir string) error {
	path := filepath.Join(kgDir, ".gitignore")
	want := []string{".cache/", EmbedderName}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	have := make(map[string]bool)
	for _, l := range strings.Split(string(existing), "\n") {
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
