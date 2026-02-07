package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/pbleisch/kg/internal/daemon"
	"github.com/spf13/cobra"
)

func init() {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background index watcher",
	}

	daemonCmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the daemon in the foreground",
			RunE: func(cmd *cobra.Command, args []string) error {
				d, err := daemon.Start(cfg.kgDir)
				if err != nil {
					return err
				}
				if !cfg.quiet {
					fmt.Println("daemon started")
				}

				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

				select {
				case <-sig:
				case <-d.Done():
				}

				d.Stop()
				if !cfg.quiet {
					fmt.Println("daemon stopped")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Signal a running daemon to stop",
			RunE: func(cmd *cobra.Command, args []string) error {
				absDir, err := filepath.Abs(cfg.kgDir)
				if err != nil {
					return err
				}
				sockPath := daemon.SocketPathFor(absDir)
				conn, err := net.Dial("unix", sockPath)
				if err != nil {
					return fmt.Errorf("daemon not running or unreachable: %w", err)
				}
				defer conn.Close()
				conn.Write([]byte("stop"))
				buf := make([]byte, 256)
				n, _ := conn.Read(buf)
				if !cfg.quiet {
					fmt.Println(string(buf[:n]))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Check if the daemon is running",
			RunE: func(cmd *cobra.Command, args []string) error {
				absDir, err := filepath.Abs(cfg.kgDir)
				if err != nil {
					return err
				}
				sockPath := daemon.SocketPathFor(absDir)
				conn, err := net.Dial("unix", sockPath)
				if err != nil {
					fmt.Println("not running")
					return nil
				}
				defer conn.Close()
				conn.Write([]byte("status"))
				buf := make([]byte, 256)
				n, _ := conn.Read(buf)
				fmt.Println(string(buf[:n]))
				return nil
			},
		},
	)

	rootCmd.AddCommand(daemonCmd)
}
