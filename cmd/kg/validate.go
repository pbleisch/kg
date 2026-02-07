package main

import (
	"fmt"
	"os"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate all entities against their schemas",
		RunE: func(cmd *cobra.Command, args []string) error {
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
		},
	}
	rootCmd.AddCommand(cmd)
}
