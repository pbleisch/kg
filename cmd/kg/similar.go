package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "similar <Type>",
		Short: "Find entities similar to a query vector",
		Long: `Find entities of a given type ranked by vector similarity.

Requires the kg-ladybug binary (LadybugDB backend).

Examples:
  kg similar Document --vector "0.1,0.2,0.3" --field embedding
  kg similar Document --like doc:intro --field embedding --top 5
  kg similar Document --vector-file query.vec --field title,body --metric cosine --top 20`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeName := args[0]

			vectorStr, _ := cmd.Flags().GetString("vector")
			vectorFile, _ := cmd.Flags().GetString("vector-file")
			likeID, _ := cmd.Flags().GetString("like")
			fieldStr, _ := cmd.Flags().GetString("field")
			metric, _ := cmd.Flags().GetString("metric")
			topN, _ := cmd.Flags().GetInt("top")

			// Exactly one query source required.
			sources := 0
			if vectorStr != "" {
				sources++
			}
			if vectorFile != "" {
				sources++
			}
			if likeID != "" {
				sources++
			}
			if sources != 1 {
				return fmt.Errorf("exactly one of --vector, --vector-file, or --like is required")
			}
			if fieldStr == "" {
				return fmt.Errorf("--field is required")
			}

			fields := splitComma(fieldStr)

			var (
				vec []float64
				err error
			)
			switch {
			case vectorStr != "":
				vec, err = query.ParseVector(vectorStr)
			case vectorFile != "":
				vec, err = query.LoadVectorFile(vectorFile)
			case likeID != "":
				vec, err = vectorFromEntity(cfg.kgDir, likeID, fields)
			}
			if err != nil {
				return err
			}

			params := store.SimilarityParams{
				TypeName: typeName,
				Vector:   vec,
				Fields:   fields,
				Metric:   metric,
				TopN:     topN,
			}

			results, err := query.Similar(cfg.kgDir, params)
			if err != nil {
				return err
			}

			// Inject synthetic _score (and _field when multiple fields
			// were searched) as attributes so existing formatters pick
			// them up as columns.
			entities := make([]*store.Entity, len(results))
			for i, r := range results {
				e := *r.Entity
				attrs := make(map[string]any, len(e.Attributes)+2)
				for k, v := range e.Attributes {
					attrs[k] = v
				}
				attrs["_score"] = fmt.Sprintf("%.6f", r.Score)
				if len(fields) > 1 {
					attrs["_field"] = r.MatchField
				}
				e.Attributes = attrs
				entities[i] = &e
			}

			f := output.NewFormatter(cfg.format)
			return f.FormatEntities(os.Stdout, entities)
		},
	}

	cmd.Flags().String("vector", "", "Query vector as comma-separated floats")
	cmd.Flags().String("vector-file", "", "Path to file containing the query vector")
	cmd.Flags().String("like", "", "Entity ID whose stored vector is used as the query")
	cmd.Flags().String("field", "", "Comma-separated attribute field name(s) to search")
	cmd.Flags().String("metric", "cosine", "Similarity metric: cosine or dot")
	cmd.Flags().Int("top", 10, "Number of results to return")

	rootCmd.AddCommand(cmd)
}

// splitComma splits a comma-separated list and trims whitespace from each part.
func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// vectorFromEntity loads an entity and returns the vector from the first
// matching field (exact name or <name>_embedding fallback).
func vectorFromEntity(kgDir, entityID string, fields []string) ([]float64, error) {
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("syncing index: %w", err)
	}
	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, err
	}
	defer idx.Close()

	e, err := idx.GetEntity(entityID)
	if err != nil {
		return nil, fmt.Errorf("entity %s not found: %w", entityID, err)
	}

	for _, field := range fields {
		for _, name := range []string{field, field + "_embedding"} {
			if val, ok := e.Attributes[name]; ok {
				if vec, ok := store.ExtractVector(val); ok {
					return vec, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("entity %s has no vector in fields %v (also tried _embedding suffixes)", entityID, fields)
}
