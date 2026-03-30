package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		apiServer string
		apiKey    string
	)

	root := &cobra.Command{
		Use:          "berth",
		Short:        "Berth CLI",
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&apiServer, "api-server", "https://localhost:8443", "Berth API server base URL")
	root.PersistentFlags().StringVar(&apiKey, "api-key", "", "Berth API key")

	leaseCmd := &cobra.Command{
		Use:   "lease",
		Short: "Lease operations",
	}

	leaseCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List leases",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "not implemented")
		},
	})

	leaseCmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Get a lease",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "not implemented")
		},
	})

	leaseCmd.AddCommand(&cobra.Command{
		Use:   "release <name>",
		Short: "Release a lease",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "not implemented")
		},
	})

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "berth version: not implemented")
		},
	}

	root.AddCommand(leaseCmd, versionCmd)

	if err := root.Execute(); err != nil {
		return 1
	}

	return 0
}
