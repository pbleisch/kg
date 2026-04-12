package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
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
				var opts []daemon.Option
				if cfg.quiet {
					opts = append(opts, daemon.WithQuiet())
				}
				d, err := daemon.Start(cfg.kgDir, opts...)
				if err != nil {
					return err
				}

				if logPath := d.LogPath(); logPath != "" && !cfg.quiet {
					fmt.Fprintf(os.Stderr, "daemon started (pid %d), logging to %s\n", os.Getpid(), logPath)
				}

				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

				select {
				case <-sig:
				case <-d.Done():
				}

				d.Stop()
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

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Show daemon log output",
		RunE: func(cmd *cobra.Command, args []string) error {
			absDir, err := filepath.Abs(cfg.kgDir)
			if err != nil {
				return err
			}
			logPath, err := daemon.LogFilePath(absDir)
			if err != nil {
				return fmt.Errorf("no daemon log found (is daemon running?)")
			}

			follow, _ := cmd.Flags().GetBool("follow")
			if follow {
				tailCmd := exec.Command("tail", "-f", logPath)
				tailCmd.Stdout = os.Stdout
				tailCmd.Stderr = os.Stderr
				return tailCmd.Run()
			}

			data, err := os.ReadFile(logPath)
			if err != nil {
				return err
			}
			os.Stdout.Write(data)
			return nil
		},
	}
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	daemonCmd.AddCommand(logsCmd)

	rootCmd.AddCommand(daemonCmd)
}
