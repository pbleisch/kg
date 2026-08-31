package main

import (
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "similar <type>",
		Short: "Find entities similar to a query vector",
		Long: `Find entities of a given type ranked by vector similarity.

Vectors live in the gitignored sidecar under .kg/.cache/, not in the entity
files; build it with 'kg sync'. The search is an exact brute-force scan of the
sidecar, so its answer does not depend on how an index was built.

Examples:
  kg similar document --vector "0.1,0.2,0.3" --field embedding
  kg similar document --like doc:intro --field embedding --top 5
  kg similar document --vector-file query.vec --field title,body --metric cosine --top 20`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeName := store.ToSnakeCase(args[0])

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
				vec, err = query.VectorForEntity(cfg.kgDir, store.NormalizeEntityID(likeID), fields)
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
				inclEmb, _ := cmd.Flags().GetBool("include-embeddings")
				if !inclEmb {
					store.StripEmbeddings(&e)
				}
				entities[i] = &e
			}

			f := output.NewFormatter(cfg.format)
			return f.FormatEntities(cmd.OutOrStdout(), entities)
		},
	}

	cmd.Flags().String("vector", "", "Query vector as comma-separated floats")
	cmd.Flags().String("vector-file", "", "Path to file containing the query vector")
	cmd.Flags().String("like", "", "Entity ID whose stored vector is used as the query")
	cmd.Flags().String("field", "", "Comma-separated attribute field name(s) to search")
	cmd.Flags().String("metric", "cosine", "Similarity metric: cosine or dot")
	cmd.Flags().Int("top", 10, "Number of results to return")
	cmd.Flags().Bool("include-embeddings", false, "Include embedding vectors in output")

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
