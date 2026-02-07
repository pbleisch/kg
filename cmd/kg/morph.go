package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/query"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "morph [name [= pattern...]]",
		Short: "List, show, or define morphisms",
		// Accept any number of args: 0 (list), 1 (show), or 3+ (define).
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0:
				return morphList()
			case len(args) >= 3 && args[1] == "=":
				name := args[0]
				pattern := strings.Join(args[2:], " ")
				desc, _ := cmd.Flags().GetString("description")
				return morphDefine(name, pattern, desc)
			case len(args) == 1:
				return morphShow(args[0])
			default:
				return fmt.Errorf("usage: kg morph [name [= pattern...]]")
			}
		},
	}
	cmd.Flags().StringP("description", "d", "", "description for the morphism")
	rootCmd.AddCommand(cmd)
}

func morphList() error {
	m, err := query.LoadMorphisms(cfg.kgDir)
	if err != nil {
		return err
	}
	if len(m) == 0 {
		fmt.Println("morphisms[0]{name,pattern}:")
		return nil
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("morphisms[%d]{name,pattern}:\n", len(names))
	for _, n := range names {
		fmt.Printf("  %s,%s\n", n, m[n].Pattern)
	}
	return nil
}

func morphDefine(name, pattern, desc string) error {
	if err := query.DefineMorphism(cfg.kgDir, name, pattern, desc); err != nil {
		return err
	}
	if !cfg.quiet {
		fmt.Printf("created: %s\n", name)
		fmt.Printf("pattern: %s\n", pattern)
	}
	return nil
}

func morphShow(name string) error {
	m, err := query.LoadMorphisms(cfg.kgDir)
	if err != nil {
		return err
	}
	morph, ok := m[name]
	if !ok {
		return &query.QueryError{
			Code:    "morphism_not_found",
			Message: fmt.Sprintf("no morphism named %q", name),
		}
	}
	fmt.Printf("name: %s\n", name)
	fmt.Printf("pattern: %s\n", morph.Pattern)
	if morph.Description != "" {
		fmt.Printf("description: %s\n", morph.Description)
	}
	return nil
}
