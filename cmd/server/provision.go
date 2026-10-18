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

// provisionOperatorCommand creates the single operator account. It is the only
// supported way to add an operator besides PocketBase superusers, and it never
// seeds credentials from code or configuration.
func provisionOperatorCommand(app core.App) *cobra.Command {
	var email string

	command := &cobra.Command{
		Use:   "provision-operator",
		Short: "Create the single operator account (one time only)",
		Long: "Create the single operator account.\n\n" +
			"The password is read from the terminal without echo, or from stdin when\n" +
			"it is piped. No credentials are seeded and a second operator is refused.",
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
			fmt.Fprintf(command.OutOrStdout(), "Operator %s provisioned. Sign in at /login.\n", strings.ToLower(strings.TrimSpace(email)))
			return nil
		},
	}
	command.Flags().StringVar(&email, "email", "", "operator email address (required)")
	_ = command.MarkFlagRequired("email")
	return command
}

// readPassword reads one password from the terminal without echo, or from a
// piped stdin (for example `echo "$PASSWORD" | bachtel provision-operator ...`).
func readPassword(command *cobra.Command) (string, error) {
	in := command.InOrStdin()
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprint(command.ErrOrStderr(), "Password: ")
		password, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(command.ErrOrStderr())
		if err != nil {
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
