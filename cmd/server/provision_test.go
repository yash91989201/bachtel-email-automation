package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

type failingConfirmationWriter struct{ err error }

func (w failingConfirmationWriter) Write([]byte) (int, error) { return 0, w.err }

// A broken output pipe must be reported without hiding that provisioning
// committed, otherwise an administrator might retry an already-completed action.
func TestProvisionReportsConfirmationWriteFailure(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	writeErr := errors.New("confirmation output unavailable")
	command := provisionOperatorCommand(app)
	command.SetArgs([]string{"--email", "operator@example.test"})
	command.SetIn(strings.NewReader("correct-horse-battery\n"))
	command.SetOut(failingConfirmationWriter{err: writeErr})
	command.SetErr(io.Discard)
	if err := command.Execute(); !errors.Is(err, writeErr) || !strings.Contains(err.Error(), "operator was provisioned") {
		t.Fatalf("provision error = %v, want an output failure explaining the committed account", err)
	}
	if count, err := app.CountRecords(operator.CollectionName); err != nil || count != 1 {
		t.Fatalf("operators = %d, err = %v; want one committed account", count, err)
	}
}
