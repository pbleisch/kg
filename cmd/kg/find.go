package main

import (
"fmt"
"os"
"strings"

"github.com/pbleisch/kg/pkg/output"
"github.com/pbleisch/kg/pkg/query"
"github.com/spf13/cobra"
)

func init() {
cmd := &cobra.Command{
Use:   "find <Type> [where <filters...>]",
Short: "Find entities by type with optional filters",
Args:  cobra.MinimumNArgs(1),
RunE: func(cmd *cobra.Command, args []string) error {
typeName := args[0]

var filters []query.Filter
rest := args[1:]
if len(rest) > 0 {
if strings.EqualFold(rest[0], "where") {
rest = rest[1:]
}
// Join remaining args and split on commas to support
// both "a=1,b=2" and "a=1" "b=2" forms.
raw := strings.Join(rest, " ")
for _, expr := range strings.Split(raw, ",") {
expr = strings.TrimSpace(expr)
if expr == "" {
continue
}
f, err := query.ParseFilter(expr)
if err != nil {
return fmt.Errorf("bad filter: %w", err)
}
filters = append(filters, f)
}
}

entities, err := query.Find(cfg.kgDir, typeName, filters)
if err != nil {
return err
}
f := output.NewFormatter(cfg.format)
return f.FormatEntities(os.Stdout, entities)
},
}
rootCmd.AddCommand(cmd)
}
