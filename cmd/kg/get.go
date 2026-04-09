package main

import (
"os"

"github.com/pbleisch/kg/pkg/output"
"github.com/pbleisch/kg/pkg/query"
"github.com/pbleisch/kg/pkg/store"
"github.com/spf13/cobra"
)

func init() {
cmd := &cobra.Command{
Use:   "get <type:id>",
Short: "Get a single entity by ID",
Args:  cobra.ExactArgs(1),
RunE: func(cmd *cobra.Command, args []string) error {
e, err := query.Get(cfg.kgDir, store.NormalizeEntityID(args[0]))
if err != nil {
return err
}
inclEmb, _ := cmd.Flags().GetBool("include-embeddings")
if !inclEmb {
store.StripEmbeddings(e)
}
f := output.NewFormatter(cfg.format)
return f.FormatEntity(os.Stdout, e)
},
}
cmd.Flags().Bool("include-embeddings", false, "Include embedding vectors in output")
rootCmd.AddCommand(cmd)
}
