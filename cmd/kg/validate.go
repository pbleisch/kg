package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate all entities against their schemas",
		RunE: func(cmd *cobra.Command, args []string) error {
			fix, _ := cmd.Flags().GetBool("fix")
			if fix {
				return runFix(cmd)
			}
			return runValidate(cmd)
		},
	}
	cmd.Flags().Bool("fix", false, "Normalize malformed entities in-place")
	rootCmd.AddCommand(cmd)
}

func runValidate(cmd *cobra.Command) error {
	schemas, err := schema.LoadAllSchemas(cfg.kgDir)
	if err != nil {
		return fmt.Errorf("loading schemas: %w", err)
	}
	entities, err := store.ListEntities(cfg.kgDir)
	if err != nil {
		return fmt.Errorf("listing entities: %w", err)
	}

	hasErrors := false
	for _, e := range entities {
		s, ok := schemas[e.Type]
		if !ok {
			continue
		}
		errs := schema.ValidateEntity(s, e)
		if len(errs) > 0 {
			hasErrors = true
			for _, ve := range errs {
				fmt.Fprintf(os.Stderr, "%s: %s: %s\n", e.ID, ve.Field, ve.Message)
			}
		}
	}
	if hasErrors {
		return fmt.Errorf("validation failed")
	}
	if !cfg.quiet {
		fmt.Println("all entities valid")
	}
	return nil
}

func runFix(cmd *cobra.Command) error {
	entitiesDir := filepath.Join(cfg.kgDir, "entities")

	var results []*store.NormalizeResult
	var warnings []string
	totalFiles := 0

	err := filepath.WalkDir(entitiesDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == entitiesDir {
				return nil
			}
			warnings = append(warnings, fmt.Sprintf("%s: %v", path, walkErr))
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		totalFiles++

		result, err := store.NormalizeEntityFile(cfg.kgDir, path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if len(result.Fixes) > 0 {
			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking entities: %w", err)
	}

	switch cfg.format {
	case output.FormatJSON:
		summary := map[string]interface{}{
			"total_files": totalFiles,
			"files_fixed": len(results),
			"warnings":    warnings,
			"results":     results,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	default:
		if len(results) == 0 && len(warnings) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "checked %d files, nothing to fix\n", totalFiles)
			return nil
		}
		for _, r := range results {
			fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", r.EntityID)
			for _, f := range r.Fixes {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", f.Field, f.Message)
			}
			if r.Moved {
				fmt.Fprintf(cmd.OutOrStdout(), "  moved: %s → %s\n", r.OldPath, r.NewPath)
			}
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\nfixed %d of %d files\n", len(results), totalFiles)
	}
	return nil
}
