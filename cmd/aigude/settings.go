package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/spf13/cobra"
)

// defaultSettingsFile is read from the working directory when --config is not
// given. It is optional: without it the defaults, environment and flags apply.
const defaultSettingsFile = "aigude.toml"

// addSettingsFlags gives a command --config and one flag per setting.
func addSettingsFlags(cmd *cobra.Command) {
	cmd.Flags().String("config", "", "settings file (default: ./"+defaultSettingsFile+" if it exists)")
	cmd.Flags().AddFlagSet(settings.NewFlagSet())
}

// loadSettings reads the settings from the file, environment and flags of a command.
func loadSettings(cmd *cobra.Command) (settings.Settings, error) {
	file, err := settingsFile(cmd)
	if err != nil {
		return settings.Settings{}, err
	}
	return settings.Load(settings.LoadOptions{File: file, Flags: cmd.Flags()})
}

// settingsFile returns the settings file to read: the one given with
// --config, which must exist, else the default file if it is there, else none.
func settingsFile(cmd *cobra.Command) (string, error) {
	if cmd.Flags().Changed("config") {
		return cmd.Flags().GetString("config")
	}
	if _, err := os.Stat(defaultSettingsFile); err == nil {
		slog.Info("Using settings file", "file", defaultSettingsFile)
		return defaultSettingsFile, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("settings file %s: %w", defaultSettingsFile, err)
	}
	slog.Info("No settings file found, using defaults, environment and flags", "file", defaultSettingsFile)
	return "", nil
}
