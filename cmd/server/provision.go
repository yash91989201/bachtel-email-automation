package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// provisionOperatorCommand creates one operator account. It is the only
// supported way to add an operator besides PocketBase superusers, and it never
// seeds credentials from code or configuration. Running it again with a new
// email adds another independent private workspace.
func provisionOperatorCommand(app core.App) *cobra.Command {
	var email string

	command := &cobra.Command{
		Use:   "provision-operator",
		Short: "Create one operator account",
		Long: "Create one operator account with an independent private workspace.\n\n" +
			"The password is read from the terminal without echo, or from stdin when\n" +
			"it is piped. No credentials are seeded, public registration stays disabled,\n" +
			"and an existing login identity is refused without changing its password.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			// The serve command runs migrations automatically; a custom command
			// has to ensure the schema exists before writing a record.
			if err := app.RunAppMigrations(); err != nil {
				return err
			}
			password, err := readPassword(command)
			if err != nil {
				return err
			}
			if err := operator.Provision(app, email, password); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Operator %s provisioned. Sign in at /login.\n", strings.ToLower(strings.TrimSpace(email))); err != nil {
				return fmt.Errorf("operator was provisioned but confirmation could not be written: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&email, "email", "", "operator email address (required)")
	if err := command.MarkFlagRequired("email"); err != nil {
		panic(err) // The flag is registered above; failure is a programming error.
	}
	return command
}

// readPassword reads one password from the terminal without echo, or from a
// piped stdin (for example `echo "$PASSWORD" | bachtel provision-operator ...`).
func readPassword(command *cobra.Command) (string, error) {
	in := command.InOrStdin()
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if _, err := fmt.Fprint(command.ErrOrStderr(), "Password: "); err != nil {
			return "", fmt.Errorf("write password prompt: %w", err)
		}
		password, readErr := term.ReadPassword(int(file.Fd()))
		_, writeErr := fmt.Fprintln(command.ErrOrStderr())
		if err := errors.Join(readErr, writeErr); err != nil {
			return "", err
		}
		return string(password), nil
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
