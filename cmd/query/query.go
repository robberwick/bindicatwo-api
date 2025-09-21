package query

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// AddQuerySubcommand wires the `query` subcommand which encapsulates the
// default behavior of fetching and printing the schedule. It accepts a runner
// callback from the root package to execute the actual logic (to avoid
// importing package main). It returns the constructed command.
func AddQuerySubcommand(root *cobra.Command, run func(cmd *cobra.Command) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query upcoming bin collections",
		Long:  "Query upcoming bin collections for an address/postcode. By default prints a human-readable list; pass --json to output JSON.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// bind flags local to this command
			_ = viper.BindPFlag("search", cmd.Flags().Lookup("search"))
			_ = viper.BindPFlag("prefer", cmd.Flags().Lookup("prefer"))
			_ = viper.BindPFlag("json", cmd.Flags().Lookup("json"))
			return run(cmd)
		},
	}

	// Flags for query command
	cmd.Flags().StringP("search", "s", "", "Search query (address or postcode). Can also be set via SEARCH env or config.")
	cmd.Flags().StringP("prefer", "p", "", "Prefer address containing this text when multiple matches. Can also be set via PREFER env or config.")
	cmd.Flags().BoolP("json", "j", false, "Output JSON instead of human-readable text. Can also be set via JSON env or config.")

	root.AddCommand(cmd)
	return cmd
}
