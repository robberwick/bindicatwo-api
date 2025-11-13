package search

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/manifoldco/promptui"
	"github.com/robberwick/bindicatwo-api/pkg/nhdc"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// bellSkipper is a writer that filters out bell characters to prevent
// terminal chimes on Windows
type bellSkipper struct {
	io.WriteCloser
}

func (bs *bellSkipper) Write(b []byte) (int, error) {
	const charBell = 7 // bell char
	var newBytes []byte
	for _, v := range b {
		if v != charBell {
			newBytes = append(newBytes, v)
		}
	}
	return bs.WriteCloser.Write(newBytes)
}

// AddSearchSubcommand adds the `search` subcommand which allows users to search for
// addresses by postcode and optionally select one to get the UPRN
func AddSearchSubcommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search for addresses by UK postcode",
		Long:  "Search for addresses by UK postcode using the Cloud9 API. Returns matching addresses with their UPRNs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// bind flags
			_ = viper.BindPFlag("postcode", cmd.Flags().Lookup("postcode"))
			_ = viper.BindPFlag("json", cmd.Flags().Lookup("json"))
			_ = viper.BindPFlag("interactive", cmd.Flags().Lookup("interactive"))

			postcode := viper.GetString("postcode")
			asJSON := viper.GetBool("json")
			interactive := viper.GetBool("interactive")

			if postcode == "" {
				return fmt.Errorf("postcode is required: provide --postcode or -p")
			}

			client := nhdc.NewClient()
			addresses, err := nhdc.SearchAddresses(client, postcode)
			if err != nil {
				return fmt.Errorf("failed to search addresses: %w", err)
			}

			if len(addresses) == 0 {
				fmt.Fprintln(os.Stderr, "No addresses found for postcode:", postcode)
				return nil
			}

			// If interactive mode, show selection prompt
			if interactive {
				selected, err := selectAddress(addresses)
				if err != nil {
					return err
				}
				fmt.Println("Selected UPRN:", selected.UPRN)
				fmt.Println("Address:", selected.FullAddress)
				return nil
			}

			// Otherwise, output as JSON or human-readable list
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(addresses)
			}

			// Human-readable output
			fmt.Printf("Found %d address(es) for postcode %s:\n\n", len(addresses), postcode)
			for i, addr := range addresses {
				fmt.Printf("%d. %s\n", i+1, addr.FullAddress)
				fmt.Printf("   UPRN: %s\n", addr.UPRN)
				if addr.AddressLine2 != "" {
					fmt.Printf("   Line 2: %s\n", addr.AddressLine2)
				}
				fmt.Println()
			}
			return nil
		},
	}

	cmd.Flags().StringP("postcode", "p", "", "UK postcode to search for (required)")
	cmd.Flags().BoolP("json", "j", false, "Output JSON instead of human-readable text")
	cmd.Flags().BoolP("interactive", "i", false, "Interactive mode: select an address from the list")

	root.AddCommand(cmd)
	return cmd
}

// selectAddress shows an interactive prompt for the user to select an address
func selectAddress(addresses []nhdc.Address) (*nhdc.Address, error) {
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no addresses to select from")
	}

	if len(addresses) == 1 {
		fmt.Fprintln(os.Stderr, "Only one address found, selecting automatically")
		return &addresses[0], nil
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "▸ {{ .FullAddress | cyan }}",
		Inactive: "  {{ .FullAddress }}",
		Selected: "✓ {{ .FullAddress | green }}",
	}

	prompt := promptui.Select{
		Label:     "Select an address",
		Items:     addresses,
		Templates: templates,
		Size:      10,
		HideHelp:  true,
		Stdout:    &bellSkipper{os.Stderr},
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return nil, fmt.Errorf("address selection failed: %w", err)
	}

	return &addresses[idx], nil
}
