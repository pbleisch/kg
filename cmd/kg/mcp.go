package main

import (
	"os"

	"github.com/pbleisch/kg/internal/mcp"
	"github.com/spf13/cobra"
)

func init() {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) server",
	}

	mcpCmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server on stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := mcp.NewServer(cfg.kgDir)
			return srv.Run(os.Stdin, os.Stdout)
		},
	})

	rootCmd.AddCommand(mcpCmd)
}
