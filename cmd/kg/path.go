package main

import (
"fmt"
"os"
"strings"

"github.com/pbleisch/kg/pkg/query"
"github.com/spf13/cobra"
)

func init() {
cmd := &cobra.Command{
Use:   "path <id> [-> <rel>]...",
Short: "Traverse forward relations from an entity",
Args:  cobra.MinimumNArgs(1),
RunE: func(cmd *cobra.Command, args []string) error {
startID := args[0]

var steps []query.PathStep
i := 1
for i < len(args) {
arrow := args[i]
recursive := false
switch arrow {
case "->":
// single hop
case "->*":
recursive = true
default:
return fmt.Errorf("expected '->' or '->*', got %q", arrow)
}
i++
if i >= len(args) {
return fmt.Errorf("missing relation name after %s", arrow)
}
steps = append(steps, query.PathStep{
Relation:  args[i],
Recursive: recursive,
})
i++
}

verbose, _ := cmd.Flags().GetBool("verbose")

if verbose {
results, err := query.PathVerbose(cfg.kgDir, startID, steps)
if err != nil {
return err
}
for _, r := range results {
fmt.Fprintf(os.Stdout, "%s%s (via %s)\n",
strings.Repeat("  ", r.Depth-1), r.ID, r.Via)
}
return nil
}

ids, err := query.Path(cfg.kgDir, startID, steps)
if err != nil {
return err
}
for _, id := range ids {
fmt.Fprintln(os.Stdout, id)
}
return nil
},
}
cmd.Flags().Bool("verbose", false, "show traversal depth and relation info")
cmd.Flags().SetInterspersed(false)
rootCmd.AddCommand(cmd)
}
