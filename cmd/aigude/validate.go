package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/alex0ptr/aigude/pkg/userconfig"
	"github.com/spf13/cobra"
)

// newValidateCommand checks the User Config files without starting the service.
func newValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check the User Config files without starting the service",
		Long: "Check the User Config files in the users folder without starting the service.\n\n" +
			"Every .toml file directly in the folder is read strictly. All problems are listed as\n" +
			"file:line:column: message and the command fails if there is any. The users folder\n" +
			"comes from the same settings as for serve (settings file, environment, --users-dir).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := runValidate(cmd)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
			}
			return err
		},
	}
	addSettingsFlags(cmd)
	return cmd
}

// runValidate loads the settings for the users folder and checks the folder.
func runValidate(cmd *cobra.Command) error {
	cfg, err := loadSettings(cmd)
	if err != nil {
		return err
	}
	slog.Debug("Validating User Config", "usersDir", cfg.UsersDir)
	snapshot, err := userconfig.LoadDir(cfg.UsersDir)
	if err != nil {
		return err
	}
	users := snapshot.Users()
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = u.DisplayName
	}
	fmt.Fprintf(cmd.OutOrStdout(), "User Config in %s is valid: %d User(s): %s\n", cfg.UsersDir, len(users), strings.Join(names, ", "))
	return nil
}
