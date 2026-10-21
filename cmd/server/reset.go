package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// resetLocalData removes one local PocketBase data directory so the next start
// recreates the final schema from scratch. It exists for the documented
// local-development schema transition; it is never run by startup, tests, or
// any automated check.
//
// The path must be given explicitly and must look like a PocketBase data
// directory. An exclusive dispatcher lock proves no worker (and no running
// server) owns the directory before anything is deleted.
func resetLocalData(dir string) error {
	if dir == "" {
		return errors.New("a data directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve the data directory: %w", err)
	}
	if abs == string(filepath.Separator) {
		return errors.New("refusing to reset the filesystem root")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("the data directory must exist: %w", err)
	}
	if !info.IsDir() {
		return errors.New("the data directory must be a directory")
	}
	if _, err := os.Stat(filepath.Join(abs, "data.db")); err != nil {
		return fmt.Errorf("%s does not look like a PocketBase data directory (data.db is missing)", abs)
	}

	release, err := sending.LockDataDirectory(abs)
	if err != nil {
		return fmt.Errorf("stop the server and its workers before resetting the data directory: %w", err)
	}
	defer func() {
		if err := release(); err != nil {
			// The reset already completed or failed on its own result; a
			// diagnostic write to stderr cannot change that outcome.
			fmt.Fprintf(os.Stderr, "release the data lock: %v\n", err) //nolint:errcheck // a stderr diagnostic cannot affect the reset outcome
		}
	}()

	// Keep the lock file's directory so the lock stays valid while the
	// remaining contents are removed entry by entry; recreating the parent is
	// then unnecessary.
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Errorf("read the data directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == ".sending.lock" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(abs, entry.Name())); err != nil {
			return fmt.Errorf("remove %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func resetLocalDataCommand() *cobra.Command {
	var dir string
	var confirmed bool

	command := &cobra.Command{
		Use:   "reset-local-data",
		Short: "Delete one local data directory for a schema reinitialize",
		Long: "Delete one local PocketBase data directory.\n\n" +
			"This irreversibly removes the local database, uploads, generated\n" +
			"downloads, mailbox connections and credentials in the selected directory.\n" +
			"Stop the server and its workers first; the command refuses while a\n" +
			"dispatcher owns the directory. Provision operators again and reconnect\n" +
			"mailboxes afterwards. It is a local-development transition tool, not a\n" +
			"production migration.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !confirmed {
				return errors.New("pass --yes to confirm the irreversible local data reset")
			}
			if err := resetLocalData(dir); err != nil {
				return err
			}
			_, err := fmt.Fprintf(command.OutOrStdout(), "Removed the contents of %s. Start the server to create the new schema, then provision operators.\n", dir)
			return err
		},
	}
	command.Flags().StringVar(&dir, "data-dir", "", "PocketBase data directory to reset (required)")
	command.Flags().BoolVar(&confirmed, "yes", false, "confirm the irreversible deletion")
	if err := command.MarkFlagRequired("data-dir"); err != nil {
		panic(err) // The flag is registered above; failure is a programming error.
	}
	return command
}
