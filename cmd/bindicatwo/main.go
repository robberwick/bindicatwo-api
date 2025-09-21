package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/robberwick/bindicatwo-api/cmd/config"
	"github.com/robberwick/bindicatwo-api/cmd/query"
	"github.com/robberwick/bindicatwo-api/cmd/serve"
	"github.com/robberwick/bindicatwo-api/pkg/configutil"
	"github.com/robberwick/bindicatwo-api/pkg/nhdc"
	"github.com/robberwick/bindicatwo-api/pkg/termfmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
)

func execute() error {
	rootCmd := &cobra.Command{
		Use:   "bindicatwo",
		Short: "Fetch North Herts bin collection schedule",
		Long:  "A CLI and HTTP service for fetching upcoming NHDC bin collections.",
	}
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "Path to config file (yaml/json/toml). Default search path: ~/.config/bindicatwo/config.{yaml|json|toml} (then current dir)")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error { return configutil.Init(cfgFile) }

	config.AddConfigSubcommand(rootCmd)
	query.AddQuerySubcommand(rootCmd, func(cmd *cobra.Command) error {
		search := viper.GetString("search")
		prefer := viper.GetString("prefer")
		asJSON := viper.GetBool("json")

		if strings.TrimSpace(search) == "" {
			return fmt.Errorf("search is required: provide --search or set SEARCH env var or set it in the config")
		}

		client := nhdc.NewClient()
		items, err := nhdc.GetSchedule(client, search, prefer)
		if err != nil {
			return err
		}
		if asJSON {
			nhdc.ComputeRelativeFields(items)
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}
		termfmt.PrintHuman(items)
		return nil
	})

	// Add HTTP server subcommand
	serve.AddServeSubcommand(rootCmd)

	return rootCmd.Execute()
}

func main() {
	if err := execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
