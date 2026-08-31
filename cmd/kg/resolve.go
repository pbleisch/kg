package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Find every pair of entities that may be the same thing",
		Long: `Compare every embedded entity against every other, over the whole corpus,
in one pass against a complete index, and report the pairs above a threshold.

It merges nothing. Deciding that two names are one thing is judgement, and a
threshold is not judgement; the output is the list a person or an agent
adjudicates, and ` + "`kg merge`" + ` applies what they decide.

It refuses rather than answering from a partial index: a corpus that does not
load is refused by the load, and an entity whose text has no vector is counted
and named before anything is compared. Run ` + "`kg sync`" + ` first.

  kg resolve                              every pair at cosine >= 0.90
  kg resolve --threshold 0.95 --limit 50  the closest 50 above 0.95
  kg resolve --fields name --cross-type   only pairs whose types differ`,
		RunE: func(c *cobra.Command, args []string) error {
			threshold, _ := c.Flags().GetFloat64("threshold")
			fieldsStr, _ := c.Flags().GetString("fields")
			limit, _ := c.Flags().GetInt("limit")
			sameType, _ := c.Flags().GetBool("same-type")
			crossType, _ := c.Flags().GetBool("cross-type")

			var fields []string
			for _, f := range strings.Split(fieldsStr, ",") {
				if f = strings.TrimSpace(f); f != "" {
					fields = append(fields, f)
				}
			}

			res, err := command.Resolve(cfg.kgDir, command.ResolveOptions{
				Threshold: threshold,
				Fields:    fields,
				SameType:  sameType,
				CrossType: crossType,
				Limit:     limit,
			})
			if err != nil {
				return err
			}

			w := c.OutOrStdout()
			switch cfg.format {
			case output.FormatJSON:
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			default:
				for _, p := range res.Pairs {
					fmt.Fprintf(w, "%.4f  %s (%s)  ~  %s (%s)  [%s]\n",
						p.Score, p.A, p.TextA, p.B, p.TextB, p.Field)
				}
				if !cfg.quiet {
					fmt.Fprintf(w, "%d candidate pair(s) at cosine >= %.2f over %d vectors from %d entities: %d same-type, %d cross-type\n",
						res.Total, res.Threshold, res.Compared, res.Entities, res.SameType, res.CrossType)
				}
			}
			return nil
		},
	}
	cmd.Flags().Float64("threshold", 0.90, "Cosine score at or above which a pair is a candidate")
	cmd.Flags().String("fields", "", "Comma-separated embedded fields to compare (default: the embedder's)")
	cmd.Flags().Int("limit", 0, "Report at most this many pairs, highest score first")
	cmd.Flags().Bool("same-type", false, "Report only pairs whose types agree")
	cmd.Flags().Bool("cross-type", false, "Report only pairs whose types differ")
	rootCmd.AddCommand(cmd)
}
