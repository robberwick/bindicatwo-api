package configutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Init initializes Viper configuration from config file, environment variables, and defaults
func Init(cfgFile string) error {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		// Search config in home directory with name "config" (without extension).
		configDir := filepath.Join(home, ".config", "bindicatwo")
		viper.AddConfigPath(configDir)
		viper.AddConfigPath(".") // also look in current directory
		viper.SetConfigName("config")
		viper.SetConfigType("yaml") // default type, but Viper will try others
	}

	// Set environment variable prefix
	viper.SetEnvPrefix("BINDICATWO")
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		// Config file not found is OK; we can work with just env vars and flags
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file was found but another error was produced
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	return nil
}
