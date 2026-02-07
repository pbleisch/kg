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
Use:   "back <id> [via <rel>]",
Short: "Find entities that reference the given entity",
Args:  cobra.MinimumNArgs(1),
RunE: func(cmd *cobra.Command, args []string) error {
targetID := args[0]
var relation string

rest := args[1:]
if len(rest) > 0 {
if strings.EqualFold(rest[0], "via") {
rest = rest[1:]
}
if len(rest) == 0 {
return fmt.Errorf("missing relation name after 'via'")
}
relation = rest[0]
}

if relation == "" {
return fmt.Errorf("relation is required: kg back <id> via <rel>")
}

entities, err := query.Back(cfg.kgDir, targetID, relation)
if err != nil {
return err
}
f := output.NewFormatter(cfg.format)
return f.FormatEntities(os.Stdout, entities)
},
}
rootCmd.AddCommand(cmd)
}
