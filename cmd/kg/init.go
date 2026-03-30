package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new knowledge graph directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := filepath.Join(cfg.kgDir, "config.yaml")
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("directory %s already contains a config.yaml; aborting to prevent overwrite", cfg.kgDir)
			}

			dirs := []string{
				filepath.Join(cfg.kgDir, "entities"),
				filepath.Join(cfg.kgDir, "schema"),
				filepath.Join(cfg.kgDir, ".cache"),
			}
			for _, d := range dirs {
				if err := os.MkdirAll(d, 0o755); err != nil {
					return fmt.Errorf("creating %s: %w", d, err)
				}
			}

			configContent := `# kg knowledge graph configuration
# Entity YAML files are stored in entities/<type>/<id>.yaml
# Schema files are stored in schema/<type>.yaml

# Index backend: auto, bolt, or ladybug
backend: auto

# Embedding configuration (uncomment to enable)
# Run 'kg embed --init' to scaffold a working setup automatically.
# embedding:
#   command: "python3 .kg/scripts/embed.py"
#   fields: [name, description]
#   batch: false
#   auto: false
`
			if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
				return fmt.Errorf("writing config.yaml: %w", err)
			}

			gitignorePath := filepath.Join(cfg.kgDir, ".cache", ".gitignore")
			gitignoreContent := "# Generated index cache — rebuild with 'kg sync --rebuild'\n*\n!.gitignore\n"
			if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
				return fmt.Errorf("writing .cache/.gitignore: %w", err)
			}

			if !cfg.quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "Initialized knowledge graph in %s\n", cfg.kgDir)
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
