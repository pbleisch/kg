package main

import (
	"encoding/json"
	"fmt"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/output"
	"github.com/pbleisch/kg/pkg/query"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check the graph: structure is enforced, content is advised",
		Long: `Report both halves of P3 over every entity file.

Structural findings refuse the load and exit non-zero: an identity two files
claim, a file whose declared identity disagrees with its location, an illegal
attribute name, a file that will not parse. There is no flag that suppresses
one — structure and content must not share a setting.

Content findings are reported and exit 0: an unknown attribute, a schema
violation, a type with no schema, and a relation target that names nothing. A
graph under construction is legitimately incomplete.

  kg validate                 report everything
  kg validate --format json   the same, for a program
  kg validate --fix           correct id/type to agree with the path and drop
                              illegal attribute names; moves no file
  kg validate --fix --fix-dangling
                              also delete relation targets that name nothing`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fix, _ := cmd.Flags().GetBool("fix")
			dangling, _ := cmd.Flags().GetBool("fix-dangling")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if fix || dangling {
				opts := command.DefaultRepair()
				opts.Dangling = dangling
				opts.DryRun = dryRun
				return runFix(cmd, opts)
			}
			return runValidate(cmd)
		},
	}
	cmd.Flags().Bool("fix", false, "Rewrite each file to agree with its path and drop illegal attribute names")
	cmd.Flags().Bool("fix-dangling", false, "With --fix, also delete relation targets that name no entity")
	cmd.Flags().Bool("dry-run", false, "With --fix, report what would change and write nothing")
	rootCmd.AddCommand(cmd)
}

func runValidate(cmd *cobra.Command) error {
	rep, err := query.Validate(cfg.kgDir)
	if err != nil {
		return err
	}

	w, e := cmd.OutOrStdout(), cmd.ErrOrStderr()
	switch cfg.format {
	case output.FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	default:
		for _, s := range rep.Structural {
			fmt.Fprintf(e, "error   %s: %s\n", s.Code, s.Error())
		}
		for _, a := range rep.Advisories {
			fmt.Fprintf(e, "warning %s: %s\n", a.Code, a.String())
		}
		if !cfg.quiet {
			fmt.Fprintf(w, "%d files, %d entities: %d structural error(s), %d advisory warning(s)\n",
				rep.Files, rep.Entities, len(rep.Structural), len(rep.Advisories))
		}
	}
	if !rep.OK() {
		// The graph does not load. Returning the library's own error type is
		// what gets this exit code 2 and a repair hint, and it keeps that
		// mapping out of this file (P7).
		return &store.LoadError{Errors: rep.Structural}
	}
	return nil
}

func runFix(cmd *cobra.Command, opts command.RepairOptions) error {
	res, err := command.Repair(cfg.kgDir, opts)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	switch cfg.format {
	case output.FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	default:
		for _, r := range res.Results {
			fmt.Fprintf(w, "%s:\n", r.EntityID)
			for _, f := range r.Fixes {
				fmt.Fprintf(w, "  %s: %s\n", f.Field, f.Message)
			}
		}
		for _, u := range res.Unfixable {
			fmt.Fprintf(cmd.ErrOrStderr(), "unfixed %s: %s\n", u.Code, u.Error())
		}
		verb := "fixed"
		if opts.DryRun {
			verb = "would fix"
		}
		fmt.Fprintf(w, "\n%s %d of %d files; %d left for a decision kg will not make\n",
			verb, res.Changed, res.Files, len(res.Unfixable))
	}
	return nil
}
