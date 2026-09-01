package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/pbleisch/kg/pkg/vector"
)

// EmbedderFile is the per-checkout embedder configuration, read from
// <kgDir>/embedder.yaml.
//
// It is deliberately not config.yaml. config.yaml travels with the graph, and
// an embedding command is not part of the graph: it names a program on one
// machine. A command committed alongside the data is either wrong on every
// other checkout or, as it was here, a CWD-relative path that only resolves
// when kg happens to be run from the one directory that has both the data and
// the virtualenv. See ADR-0025.
type EmbedderFile struct {
	Command string   `yaml:"command"`
	Fields  []string `yaml:"fields"`
	Batch   bool     `yaml:"batch"`
}

// EmbedderName is the file kg reads the embedding command from.
const EmbedderName = "embedder.yaml"

// Embedder is a resolved embedding command, with the place it came from so
// `kg sync --show` can say which of several possible sources won.
type Embedder struct {
	Command string
	Fields  []string
	Batch   bool
	Source  string
}

// Digest identifies the embedder for the vector sidecar, so that changing
// embedders invalidates vectors whose text has not changed.
//
// It covers the command string and the contents of any script the command
// names — so editing the scaffolded embed.py, which is what changing the model
// usually means locally, is detected. It cannot cover everything: a hosted
// model swapped behind the same endpoint, or a Python package upgraded under
// the same script, both leave the digest alone. `kg sync --rebuild` is the
// answer to those, and P8 is the reason kg cannot do better — the model is not
// kg's to know.
//
// The interpreter itself is deliberately not hashed: a Python point release
// would otherwise force a whole-corpus re-embed for nothing.
func (e *Embedder) Digest() string {
	if e == nil {
		return ""
	}
	material := e.Command
	if parts, err := shellSplit(e.Command); err == nil {
		for _, arg := range parts[1:] {
			if !filepath.IsAbs(arg) {
				continue
			}
			info, err := os.Stat(arg)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			data, err := os.ReadFile(arg)
			if err != nil {
				continue
			}
			material += "\x00" + arg + "\x00" + string(data)
		}
	}
	return vector.EmbedderDigest(material)
}

// EmbedderPath names the embedder file for a graph directory.
func EmbedderPath(kgDir string) string { return filepath.Join(kgDir, EmbedderName) }

// ErrNoEmbedder reports that no embedding command is configured. It is a
// distinct type because "not configured" and "configured but broken" call for
// different messages, and conflating them is how `kg sync` used to stop
// embedding without saying so.
type ErrNoEmbedder struct{ KgDir string }

func (e *ErrNoEmbedder) Error() string {
	return fmt.Sprintf("no embedding command configured: set KG_EMBED_COMMAND, or write %s (see `kg sync --init`)",
		EmbedderPath(e.KgDir))
}

// ResolveEmbedder finds the embedding command for a graph, first hit wins:
//
//  1. KG_EMBED_COMMAND, with KG_EMBED_FIELDS and KG_EMBED_BATCH. This is how a
//     pipeline in the code repository points kg at its own virtualenv without
//     writing anything into the data repository.
//  2. <kgDir>/embedder.yaml — gitignored, written per checkout, absolute.
//
// It returns *ErrNoEmbedder when neither is present, and a plain error when
// what it found cannot be used. It never returns a command it has not checked
// is resolvable: silence is the failure mode this function exists to remove.
func ResolveEmbedder(kgDir string) (*Embedder, error) {
	if err := rejectGraphEmbedding(kgDir); err != nil {
		return nil, err
	}

	if cmd := strings.TrimSpace(os.Getenv("KG_EMBED_COMMAND")); cmd != "" {
		e := &Embedder{
			Command: cmd,
			Fields:  splitFields(os.Getenv("KG_EMBED_FIELDS")),
			Batch:   envBool("KG_EMBED_BATCH"),
			Source:  "KG_EMBED_COMMAND",
		}
		return validateEmbedder(e)
	}

	path := EmbedderPath(kgDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ErrNoEmbedder{KgDir: kgDir}
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f EmbedderFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if strings.TrimSpace(f.Command) == "" {
		return nil, fmt.Errorf("%s names no command", path)
	}
	return validateEmbedder(&Embedder{
		Command: f.Command,
		Fields:  f.Fields,
		Batch:   f.Batch,
		Source:  path,
	})
}

// rejectGraphEmbedding refuses a graph that still carries an embedding block
// in config.yaml. Ignoring it would leave the operator with a file that reads
// as configuration and does nothing — which is the failure this whole change
// is about. The error says exactly what to do instead.
func rejectGraphEmbedding(kgDir string) error {
	data, err := os.ReadFile(filepath.Join(kgDir, "config.yaml"))
	if err != nil {
		return nil // no config.yaml is fine; it configures nothing kg reads
	}
	var probe struct {
		Embedding *struct {
			Command string   `yaml:"command"`
			Fields  []string `yaml:"fields"`
			Batch   bool     `yaml:"batch"`
		} `yaml:"embedding"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil || probe.Embedding == nil {
		return nil
	}
	fields := strings.Join(probe.Embedding.Fields, ",")
	if fields == "" {
		fields = "name,description"
	}
	return fmt.Errorf(`%s still carries an "embedding:" block; kg no longer reads it (ADR-0025).

The embedding command names a program on one machine, so it cannot travel with
the graph. Move it out and delete the block:

  cat > %s <<'YAML'
  command: %q   # make this an absolute path
  fields: [%s]
  batch: %v
  YAML

or, from a pipeline, export KG_EMBED_COMMAND instead. Add %s to the graph
repository's .gitignore: it is per-checkout, not shared`,
		filepath.Join(kgDir, "config.yaml"),
		EmbedderPath(kgDir),
		probe.Embedding.Command,
		fields,
		probe.Embedding.Batch,
		EmbedderName)
}

// validateEmbedder checks that the command can actually be run, and in
// particular that it does not depend on the caller's working directory.
//
// A CWD-relative command is refused rather than resolved. Resolving it against
// kgDir would work today and break the moment the graph moves, and resolving
// it against the CWD is the bug: `kg sync` from one directory would embed and
// from another would not.
func validateEmbedder(e *Embedder) (*Embedder, error) {
	parts, err := shellSplit(e.Command)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.Source, err)
	}
	prog := parts[0]
	switch {
	case filepath.IsAbs(prog):
		if _, err := os.Stat(prog); err != nil {
			return nil, fmt.Errorf("%s: embedding command %s: %w", e.Source, prog, err)
		}
	case strings.ContainsRune(prog, filepath.Separator):
		return nil, fmt.Errorf(
			"%s: embedding command %q is relative to the working directory, so it resolves differently depending on where kg is run; use an absolute path or a program on PATH",
			e.Source, prog)
	default:
		if _, err := exec.LookPath(prog); err != nil {
			return nil, fmt.Errorf("%s: embedding command %q is not on PATH: %w", e.Source, prog, err)
		}
	}

	// Arguments are checked too: `python .kg/scripts/embed.py` is exactly as
	// CWD-dependent as `.venv/bin/python` is, and fails just as quietly.
	for _, arg := range parts[1:] {
		if strings.HasPrefix(arg, "-") || !strings.ContainsRune(arg, filepath.Separator) {
			continue
		}
		if !filepath.IsAbs(arg) {
			return nil, fmt.Errorf(
				"%s: embedding command argument %q is a working-directory-relative path; use an absolute path",
				e.Source, arg)
		}
	}

	if len(e.Fields) == 0 {
		return nil, fmt.Errorf("%s: no fields to embed; set fields (for example [name, description])", e.Source)
	}
	return e, nil
}

func splitFields(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envBool(name string) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(name)))
	return err == nil && v
}
