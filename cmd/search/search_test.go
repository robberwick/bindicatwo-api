package search

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestAddSearchSubcommand_RegistersAndFlags(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	cmd := AddSearchSubcommand(root)

	if cmd == nil {
		t.Fatal("AddSearchSubcommand returned nil")
	}
	if cmd.Use != "search" {
		t.Errorf("expected Use='search', got %q", cmd.Use)
	}

	// Check that the command was added to root
	found := false
	for _, c := range root.Commands() {
		if c.Use == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Error("search command was not added to root")
	}

	// Check flags
	if cmd.Flags().Lookup("postcode") == nil {
		t.Error("postcode flag not registered")
	}
	if cmd.Flags().Lookup("json") == nil {
		t.Error("json flag not registered")
	}
	if cmd.Flags().Lookup("interactive") == nil {
		t.Error("interactive flag not registered")
	}

	// Check short flags
	if cmd.Flags().ShorthandLookup("p") == nil {
		t.Error("postcode short flag -p not registered")
	}
	if cmd.Flags().ShorthandLookup("j") == nil {
		t.Error("json short flag -j not registered")
	}
	if cmd.Flags().ShorthandLookup("i") == nil {
		t.Error("interactive short flag -i not registered")
	}
}

func TestAddSearchSubcommand_MissingPostcode(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	cmd := AddSearchSubcommand(root)

	// Call RunE directly without setting postcode
	err := cmd.RunE(cmd, []string{})
	if err == nil {
		t.Fatal("expected error when postcode is not provided")
	}
	if err.Error() != "postcode is required: provide --postcode or -p" {
		t.Errorf("unexpected error message: %v", err)
	}
}
