package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

func defaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, ".config", "bindicatwo")
}

func defaultConfigBaseName() string { return "config" }

func defaultConfigPathWithExt(ext string) string {
	dir := defaultConfigDir()
	name := defaultConfigBaseName()
	if ext == "" {
		ext = "yaml"
	}
	return filepath.Join(dir, name+"."+ext)
}

// AddConfigSubcommand adds a `config` management command with common subcommands:
//   - init: create a new config file (default: ~/.config/bindicatwo/config.yaml)
//   - set: set a key (uprn, json, firmware_enabled, firmware_version, firmware_file)
//   - get: get a key or print all
//   - unset: delete a key from the config
//   - path: print the config file path in use
func AddConfigSubcommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration file (uprn/json/firmware)",
		Long:  "Create and edit the bindicatwo configuration using Viper-compatible formats.",
	}

	// `config init`
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			force, _ := cmd.Flags().GetBool("force")

			if err := ensureConfigTarget(format); err != nil {
				return err
			}

			cfgPath := configPath()
			// If exists and not force -> error
			if fileExists(cfgPath) && !force {
				return fmt.Errorf("config already exists at %s (use --force to overwrite)", cfgPath)
			}

			// Interactive prompts for initial values using promptui
			// Check if values were provided via flags first
			uprnFlagProvided := cmd.Flags().Changed("uprn")
			uprnFlag, _ := cmd.Flags().GetString("uprn")
			jsonFlagProvided := cmd.Flags().Changed("json")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			firmwareEnableFlagProvided := cmd.Flags().Changed("firmware-enable")
			firmwareEnableFlag, _ := cmd.Flags().GetBool("firmware-enable")
			firmwareVersionFlagProvided := cmd.Flags().Changed("firmware-version")
			firmwareVersionFlag, _ := cmd.Flags().GetString("firmware-version")
			firmwareFileFlagProvided := cmd.Flags().Changed("firmware-file")
			firmwareFileFlag, _ := cmd.Flags().GetString("firmware-file")

			var uprnIn string
			var firmwareEnabledFlag bool
			var firmwareVersionIn string
			var firmwareFileIn string

			// UPRN prompt with validation (skip if flag provided)
			if uprnFlagProvided {
				uprnIn = uprnFlag
			} else {
				// Prompt for UPRN, allowing "?" to trigger postcode search
				for uprnIn == "" {
					uprnPrompt := promptui.Prompt{
						Label:   "Enter default UPRN (or '?' to search by postcode)",
						Default: "",
						Stdin:   os.Stdin,
						Stdout:  os.Stderr,
						Validate: func(input string) error {
							input = strings.TrimSpace(input)
							// Allow empty or "?" or valid UPRN
							if input == "" || input == "?" {
								return nil
							}
							if len(input) < 8 {
								return errors.New("UPRN must be at least 8 characters")
							}
							return nil
						},
					}
					result, err := uprnPrompt.Run()
					if err != nil {
						return fmt.Errorf("UPRN prompt failed: %w", err)
					}
					result = strings.TrimSpace(result)

					// If user enters "?", trigger postcode search
					if result == "?" {
						// Prompt for postcode
						postcodePrompt := promptui.Prompt{
							Label:  "Enter UK postcode",
							Stdin:  os.Stdin,
							Stdout: os.Stderr,
							Validate: func(input string) error {
								input = strings.TrimSpace(input)
								if input == "" {
									return errors.New("postcode cannot be empty")
								}
								return nil
							},
						}
						postcode, err := postcodePrompt.Run()
						if err != nil {
							return fmt.Errorf("postcode prompt failed: %w", err)
						}

						// Search for addresses
						client := nhdc.NewClient()
						addresses, err := nhdc.SearchAddresses(client, postcode)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Failed to search addresses: %v\n", err)
							// Loop back to UPRN prompt
							continue
						}

						if len(addresses) == 0 {
							fmt.Fprintln(os.Stderr, "No addresses found for postcode:", postcode)
							// Loop back to UPRN prompt
							continue
						}

						// Select address from list
						selected, err := selectAddressForConfig(addresses)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Address selection failed: %v\n", err)
							// Loop back to UPRN prompt
							continue
						}
						uprnIn = selected.UPRN
						fmt.Fprintf(os.Stderr, "Selected: %s (UPRN: %s)\n", selected.FullAddress, selected.UPRN)
					} else {
						// User entered a UPRN directly or left it empty
						uprnIn = result
					}
				}
			}

			// JSON output confirm prompt (skip if flag provided)
			if !jsonFlagProvided {
				jsonPrompt := promptui.Prompt{
					Label:     "Default output JSON",
					IsConfirm: true,
					Stdin:     os.Stdin,
					Stdout:    os.Stderr,
				}
				jsonResult, err := jsonPrompt.Run()
				if err != nil && err != promptui.ErrAbort {
					return fmt.Errorf("JSON prompt failed: %w", err)
				}
				// IsConfirm returns "y" or error (ErrAbort for "n")
				jsonFlag = (err == nil && strings.EqualFold(jsonResult, "y"))
			}

			// Firmware enabled confirm prompt (skip if flag provided)
			if firmwareEnableFlagProvided {
				firmwareEnabledFlag = firmwareEnableFlag
			} else {
				firmwareEnabledPrompt := promptui.Prompt{
					Label:     "Enable firmware OTA endpoints",
					IsConfirm: true,
					Stdin:     os.Stdin,
					Stdout:    os.Stderr,
				}
				firmwareEnabledResult, err := firmwareEnabledPrompt.Run()
				if err != nil && err != promptui.ErrAbort {
					return fmt.Errorf("firmware enabled prompt failed: %w", err)
				}
				firmwareEnabledFlag = (err == nil && strings.EqualFold(firmwareEnabledResult, "y"))
			}

			// Firmware version prompt (skip if flag provided)
			if firmwareVersionFlagProvided {
				firmwareVersionIn = firmwareVersionFlag
			} else {
				firmwareVersionPrompt := promptui.Prompt{
					Label:   "Enter firmware version string (optional)",
					Default: "",
					Stdin:   os.Stdin,
					Stdout:  os.Stderr,
				}
				result, err := firmwareVersionPrompt.Run()
				if err != nil {
					return fmt.Errorf("firmware version prompt failed: %w", err)
				}
				firmwareVersionIn = strings.TrimSpace(result)
			}

			// Firmware file path prompt (skip if flag provided)
			if firmwareFileFlagProvided {
				firmwareFileIn = firmwareFileFlag
			} else {
				firmwareFilePrompt := promptui.Prompt{
					Label:   "Enter firmware binary file path (optional)",
					Default: "",
					Stdin:   os.Stdin,
					Stdout:  os.Stderr,
				}
				result, err := firmwareFilePrompt.Run()
				if err != nil {
					return fmt.Errorf("firmware file prompt failed: %w", err)
				}
				firmwareFileIn = strings.TrimSpace(result)
			}

			// Set collected values (including empty strings)
			viper.Set("uprn", uprnIn)
			viper.Set("json", jsonFlag)
			viper.Set("firmware_enabled", firmwareEnabledFlag)
			viper.Set("firmware_version", firmwareVersionIn)
			viper.Set("firmware_file", firmwareFileIn)

			// Write new file (overwrite if --force)
			viper.SetConfigFile(cfgPath)
			viper.SetConfigType(configTypeFromExt(cfgPath))
			if force {
				return viper.WriteConfigAs(cfgPath)
			}
			if err := viper.SafeWriteConfigAs(cfgPath); err != nil {
				// If fails due to exists (race), try WriteConfigAs
				if _, ok := err.(viper.ConfigFileAlreadyExistsError); ok {
					return viper.WriteConfigAs(cfgPath)
				}
				return err
			}
			return nil
		},
	}
	initCmd.Flags().String("format", "yaml", "Config format: yaml|json|toml")
	initCmd.Flags().Bool("force", false, "Overwrite existing file if present")
	initCmd.Flags().String("uprn", "", "Initial default for UPRN")
	initCmd.Flags().Bool("json", false, "Initial default: output JSON")
	initCmd.Flags().Bool("firmware-enable", false, "Enable firmware OTA endpoints")
	initCmd.Flags().String("firmware-version", "", "Firmware version string")
	initCmd.Flags().String("firmware-file", "", "Path to firmware binary file")
	cmd.AddCommand(initCmd)

	// `config set <key> <value>`
	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration key (uprn, json, firmware_enabled, firmware_version, firmware_file)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfigFile(); err != nil {
				return err
			}
			key := normalizeKey(args[0])
			val := args[1]
			if key == "json" || key == "firmware_enabled" {
				v := strings.EqualFold(val, "true") || val == "1" || strings.EqualFold(val, "yes")
				viper.Set(key, v)
			} else {
				viper.Set(key, val)
			}
			return writeBack()
		},
	}
	cmd.AddCommand(setCmd)

	// `config get [key]`
	getCmd := &cobra.Command{
		Use:   "get [key]",
		Short: "Get a configuration value or all values",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = viper.ReadInConfig() // best-effort
			if len(args) == 0 {
				// Print all known keys we care about
				fmt.Printf("uprn: %s\n", viper.GetString("uprn"))
				fmt.Printf("json: %v\n", viper.GetBool("json"))
				fmt.Printf("firmware_enabled: %v\n", viper.GetBool("firmware_enabled"))
				fmt.Printf("firmware_version: %s\n", viper.GetString("firmware_version"))
				fmt.Printf("firmware_file: %s\n", viper.GetString("firmware_file"))
				fmt.Printf("file: %s\n", viper.ConfigFileUsed())
				return nil
			}
			key := normalizeKey(args[0])
			switch key {
			case "uprn":
				fmt.Println(viper.GetString("uprn"))
			case "json", "firmware_enabled":
				fmt.Println(viper.GetBool(key))
			case "firmware_version", "firmware_file":
				fmt.Println(viper.GetString(key))
			default:
				return fmt.Errorf("unknown key: %s", key)
			}
			return nil
		},
	}
	cmd.AddCommand(getCmd)

	// `config unset <key>`
	unsetCmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Delete a key from the configuration file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfigFile(); err != nil {
				return err
			}
			key := normalizeKey(args[0])
			if err := deleteKey(key); err != nil {
				return err
			}
			return writeBack()
		},
	}
	cmd.AddCommand(unsetCmd)

	// `config path`
	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the path to the config file in use",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := viper.ReadInConfig(); err == nil {
				fmt.Println(viper.ConfigFileUsed())
				return nil
			}
			fmt.Println(configPath())
			return nil
		},
	}
	cmd.AddCommand(pathCmd)

	// API key management subtree
	addAPIKeysSubcommands(cmd)

	root.AddCommand(cmd)
	return cmd
}

// --- API key management ---

func addAPIKeysSubcommands(parent *cobra.Command) {
	api := &cobra.Command{
		Use:   "api-keys",
		Short: "Manage API keys for the HTTP server",
		Long:  "Generate and manage API keys used to authenticate requests to the serve endpoint.",
	}

	// list
	list := &cobra.Command{
		Use:   "list",
		Short: "List configured API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = viper.ReadInConfig()
			keys := getAPIKeysFromViper()
			if len(keys) == 0 {
				fmt.Println("(no api keys configured)")
				return nil
			}
			for _, k := range keys {
				fmt.Println(k)
			}
			return nil
		},
	}
	api.AddCommand(list)

	// add <key>
	add := &cobra.Command{
		Use:   "add <key>",
		Short: "Add an API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfigFile(); err != nil {
				return err
			}
			keys := getAPIKeysFromViper()
			key := strings.TrimSpace(args[0])
			if key == "" {
				return fmt.Errorf("empty key")
			}
			for _, k := range keys {
				if k == key {
					return fmt.Errorf("key already exists")
				}
			}
			keys = append(keys, key)
			return setAPIKeys(keys)
		},
	}
	api.AddCommand(add)

	// remove <key>
	remove := &cobra.Command{
		Use:   "remove <key>",
		Short: "Remove an API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfigFile(); err != nil {
				return err
			}
			want := strings.TrimSpace(args[0])
			if want == "" {
				return fmt.Errorf("empty key")
			}
			keys := getAPIKeysFromViper()
			out := make([]string, 0, len(keys))
			for _, k := range keys {
				if k != want {
					out = append(out, k)
				}
			}
			if len(out) == len(keys) {
				return fmt.Errorf("key not found")
			}
			return setAPIKeys(out)
		},
	}
	api.AddCommand(remove)

	// generate
	gen := &cobra.Command{
		Use:   "generate",
		Short: "Generate a random API key and add it",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfigFile(); err != nil {
				return err
			}
			length, _ := cmd.Flags().GetInt("length")
			if length <= 0 {
				length = 32
			}
			key, err := generateAPIKey(length)
			if err != nil {
				return err
			}
			keys := getAPIKeysFromViper()
			keys = append(keys, key)
			if err := setAPIKeys(keys); err != nil {
				return err
			}
			fmt.Println(key)
			return nil
		},
	}
	gen.Flags().Int("length", 32, "Key length in bytes before hex encoding (default 32 => 64 hex chars)")
	api.AddCommand(gen)

	parent.AddCommand(api)
}

func getAPIKeysFromViper() []string {
	keys := []string{}
	v := viper.Get("api_keys")
	switch t := v.(type) {
	case []string:
		for _, k := range t {
			k = strings.TrimSpace(k)
			if k != "" {
				keys = append(keys, k)
			}
		}
	case []any:
		for _, it := range t {
			if s, ok := it.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					keys = append(keys, s)
				}
			}
		}
	case string:
		// Support CSV from env
		csv := strings.TrimSpace(t)
		if csv != "" {
			parts := strings.Split(csv, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					keys = append(keys, p)
				}
			}
		}
	}
	if s := strings.TrimSpace(viper.GetString("api_key")); s != "" {
		keys = append(keys, s)
	}
	// dedupe
	if len(keys) > 1 {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(keys))
		for _, k := range keys {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
		keys = out
	}
	return keys
}

func setAPIKeys(keys []string) error {
	// Clean and sort for stability
	clean := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			clean = append(clean, k)
		}
	}
	viper.Set("api_keys", clean)
	viper.Set("api_key", "")
	return writeBack()
}

func generateAPIKey(nbytes int) (string, error) {
	b := make([]byte, nbytes)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ensureConfigTarget(format string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "yaml"
	}
	switch format {
	case "yaml", "yml", "json", "toml":
		// ok
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
	// If a custom --config was specified at root, we honor its extension.
	cfgUsed := viper.ConfigFileUsed()
	if cfgUsed != "" {
		return nil
	}
	// Otherwise, choose ~/.config/bindicatwo/config.<ext>
	ext := extFromFormat(format)
	p := defaultConfigPathWithExt(ext)
	// ensure directory exists
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	viper.SetConfigFile(p)
	return nil
}

func configPath() string {
	if v := viper.ConfigFileUsed(); v != "" {
		return v
	}
	// Default location: ~/.config/bindicatwo/config.yaml
	return filepath.Clean(defaultConfigPathWithExt("yaml"))
}

func configTypeFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	default:
		return "yaml"
	}
}

func extFromFormat(format string) string {
	switch strings.ToLower(format) {
	case "yaml", "yml":
		return "yaml"
	case "json":
		return "json"
	case "toml":
		return "toml"
	default:
		return "yaml"
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func requireConfigFile() error {
	// If a config has been loaded, use it; otherwise, try to read default, or create it.
	if err := viper.ReadInConfig(); err == nil {
		return nil
	}
	p := configPath()
	// Ensure directory exists
	dir := filepath.Dir(p)
	_ = os.MkdirAll(dir, 0o755)
	// If doesn't exist, create an empty file first.
	if !fileExists(p) {
		viper.SetConfigFile(p)
		viper.SetConfigType(configTypeFromExt(p))
		// write empty map
		if err := viper.SafeWriteConfigAs(p); err != nil {
			// If SafeWrite fails because exists (race), continue
			// Else try WriteConfigAs as a fallback
			_ = viper.WriteConfigAs(p)
		}
	}
	return viper.ReadInConfig()
}

func writeBack() error {
	p := viper.ConfigFileUsed()
	if p == "" {
		p = configPath()
	}
	// Ensure directory exists
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	viper.SetConfigFile(p)
	viper.SetConfigType(configTypeFromExt(p))
	if !fileExists(p) {
		return viper.WriteConfigAs(p)
	}
	return viper.WriteConfig()
}

func normalizeKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	switch k {
	case "uprn", "json", "firmware_enabled", "firmware_version", "firmware_file":
		return k
	default:
		return k
	}
}

func deleteKey(key string) error {
	key = normalizeKey(key)
	if key == "" {
		return fmt.Errorf("empty key")
	}
	// Make a settings map, remove the key, and reload into Viper before writing.
	settings := viper.AllSettings()
	delete(settings, key)
	// Now we need to replace Viper's internal config with this map.
	v := viper.GetViper()
	v.Set("__placeholder__", 0) // ensure v has at least one key
	// Reset by creating a new Viper is overkill; instead, set known keys.
	for k := range v.AllSettings() {
		v.Set(k, nil)
	}
	for k, val := range settings {
		v.Set(k, val)
	}
	// Best-effort: some formats may retain nulls; WriteConfig will serialize current view.
	return nil
}

// selectAddressForConfig shows an interactive prompt for the user to select an address during config init
func selectAddressForConfig(addresses []nhdc.Address) (*nhdc.Address, error) {
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
		return nil, err
	}

	return &addresses[idx], nil
}
