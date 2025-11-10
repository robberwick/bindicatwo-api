package query

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRoot() *cobra.Command {
	return &cobra.Command{Use: "bindicatwo"}
}

func TestAddQuerySubcommand_RegistersAndFlags(t *testing.T) {
	root := newRoot()
	cmd := AddQuerySubcommand(root, func(cmd *cobra.Command) error { return nil })

	// Ensure returned command is wired to root
	found := root.Commands()
	if len(found) == 0 {
		t.Fatalf("expected at least one subcommand on root")
	}
	if cmd.Use != "query" {
		t.Fatalf("expected Use 'query', got %q", cmd.Use)
	}

	// Verify flags exist with expected default values
	if f := cmd.Flags().Lookup("uprn"); f == nil || f.Shorthand != "u" || f.DefValue != "" {
		t.Fatalf("uprn flag incorrect: %+v", f)
	}
	if f := cmd.Flags().Lookup("json"); f == nil || f.Shorthand != "j" || f.DefValue != "false" {
		t.Fatalf("json flag incorrect: %+v", f)
	}
}

func TestQueryRun_BindsFlagsAndCallsRunner(t *testing.T) {
	viper.Reset()
	root := newRoot()
	called := 0
	cmd := AddQuerySubcommand(root, func(c *cobra.Command) error {
		called++
		// After binding, viper should reflect flag values
		if got := viper.GetString("uprn"); got != "100080795976" {
			t.Fatalf("expected uprn bound into viper, got %q", got)
		}
		if got := viper.GetBool("json"); !got {
			t.Fatalf("expected json=true bound into viper")
		}
		return nil
	})

	// Execute as: bindicatwo query --uprn 100080795976 --json
	root.SetArgs([]string{"query", "--uprn", "100080795976", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	if called != 1 {
		t.Fatalf("runner should be called exactly once, got %d", called)
	}

	// Also ensure flags stayed set on the command object
	if f := cmd.Flags().Lookup("uprn"); f == nil || f.Value.String() != "100080795976" {
		t.Fatalf("uprn flag value unexpected: %v", f)
	}
}

func TestQueryRun_ShortFlags(t *testing.T) {
	viper.Reset()
	root := newRoot()
	called := 0
	AddQuerySubcommand(root, func(c *cobra.Command) error {
		called++
		if v := viper.GetString("uprn"); v != "SHORT123" {
			return errors.New("uprn short flag not bound")
		}
		if !viper.GetBool("json") {
			return errors.New("json short flag not bound")
		}
		return nil
	})
	root.SetArgs([]string{"query", "-u", "SHORT123", "-j"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute short flags: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected runner called once, got %d", called)
	}
}

func TestQueryRun_Defaults(t *testing.T) {
	viper.Reset()
	root := newRoot()
	AddQuerySubcommand(root, func(c *cobra.Command) error {
		if v := viper.GetString("uprn"); v != "" {
			return errors.New("expected empty uprn by default")
		}
		if v := viper.GetBool("json"); v {
			return errors.New("expected json=false by default")
		}
		return nil
	})
	root.SetArgs([]string{"query"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute defaults: %v", err)
	}
}

func TestQueryRun_MultipleExecutions(t *testing.T) {
	viper.Reset()
	root := newRoot()
	calls := 0
	AddQuerySubcommand(root, func(c *cobra.Command) error {
		calls++
		return nil
	})
	root.SetArgs([]string{"query"})
	if err := root.Execute(); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	root.SetArgs([]string{"query", "--uprn", "X"})
	if err := root.Execute(); err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestQueryRun_ErrorPropagation(t *testing.T) {
	viper.Reset()
	root := newRoot()
	runErr := errors.New("boom")
	_ = AddQuerySubcommand(root, func(c *cobra.Command) error { return runErr })

	root.SetArgs([]string{"query"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error from runner to propagate")
	}
	if err.Error() != runErr.Error() {
		t.Fatalf("unexpected error propagated: %v", err)
	}
}
