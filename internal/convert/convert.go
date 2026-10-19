// Package convert turns an uploaded DOCX body document into semantic HTML with
// a packaged Pandoc executable.
//
// The conversion is intentionally narrow: one fixed reader, one fixed writer,
// fixed arguments, a private temporary directory, a deadline, and bounded input
// and output. No shell is involved, no operator-supplied filter, template or
// resource URL ever reaches the process, and Pandoc's own sandbox restricts its
// file access to the document it was given.
package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultExecutable is the executable name used when no explicit path is
	// configured.
	DefaultExecutable = "pandoc"
	// DefaultTimeout bounds one conversion.
	DefaultTimeout = 30 * time.Second
	// MaxOutputBytes bounds the converted HTML.
	MaxOutputBytes = 10 << 20
	// maxErrorBytes bounds captured converter diagnostics.
	maxErrorBytes = 4 << 10
)

// Errors reported by this package.
var (
	// ErrConverterMissing reports that no usable Pandoc executable was found.
	ErrConverterMissing = errors.New("the DOCX converter (pandoc) is not available")
	// ErrInputTooLarge reports a document above the accepted size.
	ErrInputTooLarge = fmt.Errorf("the document exceeds the %d byte limit", MaxDocumentBytes)
	// ErrOutputTooLarge reports converted HTML above the accepted size.
	ErrOutputTooLarge = fmt.Errorf("the converted document exceeds the %d byte limit", MaxOutputBytes)
	// ErrNotDocx reports an upload that is not a readable DOCX package.
	ErrNotDocx = errors.New("the upload is not a readable DOCX document")
	// ErrNotAllowed reports document content that is refused outright.
	ErrNotAllowed = errors.New("the document contains unsupported content")
)

// Converter runs the packaged Pandoc executable. The zero value is usable and
// resolves DefaultExecutable at every call, so a missing converter is reported
// to the operator instead of preventing the process from starting.
type Converter struct {
	// Path overrides the executable. Empty means DefaultExecutable on PATH.
	Path string
	// Timeout bounds one conversion. Zero means DefaultTimeout.
	Timeout time.Duration
}

// Executable resolves the Pandoc path and reports whether it can be run.
func (c Converter) Executable() (string, error) {
	name := strings.TrimSpace(c.Path)
	if name == "" {
		name = DefaultExecutable
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: install it or set PANDOC_PATH (looked for %q)", ErrConverterMissing, name)
	}
	return resolved, nil
}

// Version returns the converter identity recorded with a revision. It is
// deliberately a short single line: the executable path is not content.
func (c Converter) Version(ctx context.Context) (string, error) {
	resolved, err := c.Executable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	out := limitedBuffer{limit: maxErrorBytes}
	cmd := exec.CommandContext(ctx, resolved, "--version")
	cmd.Env = converterEnv("")
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("read converter version: %w", err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out.String()), "\n")
	if first == "" {
		return "", errors.New("the DOCX converter reported no version")
	}
	return first, nil
}

// DOCXToHTML converts one DOCX document to semantic HTML. The document must
// already be size-checked by the caller; the limit is re-checked here so the
// converter is safe on its own.
func (c Converter) DOCXToHTML(ctx context.Context, document []byte) (result string, err error) {
	if len(document) == 0 {
		return "", ErrNotDocx
	}
	if int64(len(document)) > MaxDocumentBytes {
		return "", ErrInputTooLarge
	}
	resolved, err := c.Executable()
	if err != nil {
		return "", err
	}

	// A private directory holds the only file the converter may read. Pandoc's
	// sandbox limits further reads, and HOME is redirected so no user-level
	// Pandoc configuration or filter can be picked up.
	dir, err := os.MkdirTemp("", "bachtel-docx-")
	if err != nil {
		return "", fmt.Errorf("create conversion directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			result = ""
			err = errors.Join(err, fmt.Errorf("remove conversion directory: %w", cleanupErr))
		}
	}()
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure conversion directory: %w", err)
	}
	input := filepath.Join(dir, "body.docx")
	if err := os.WriteFile(input, document, 0o600); err != nil {
		return "", fmt.Errorf("write conversion input: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	stdout := limitedBuffer{limit: MaxOutputBytes}
	stderr := limitedBuffer{limit: maxErrorBytes}
	cmd := exec.CommandContext(runCtx, resolved,
		"--from=docx",
		"--to=html5",
		// --no-highlight works on every Pandoc 3.x; the newer
		// --syntax-highlighting=none spelling is rejected by older 3.x
		// releases (verified with 3.6.4). The deprecation notice newer
		// Pandoc prints goes to stderr and is only surfaced if the
		// conversion itself fails.
		"--no-highlight",
		"--wrap=none",
		"--sandbox",
		"--output=-",
		input,
	)
	cmd.Dir = dir
	cmd.Env = converterEnv(dir)
	cmd.Stdin = nil
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second

	runErr := cmd.Run()
	if stdout.overflowed {
		return "", ErrOutputTooLarge
	}
	if runErr != nil {
		if runCtx.Err() != nil {
			return "", fmt.Errorf("the converter did not finish within %s", c.timeout())
		}
		return "", fmt.Errorf("%w: %s", ErrNotDocx, strings.TrimSpace(stderr.String()))
	}
	html := stdout.String()
	if strings.TrimSpace(html) == "" {
		return "", fmt.Errorf("%w: the converter produced no content", ErrNotDocx)
	}
	return html, nil
}

func (c Converter) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// converterEnv is the minimal environment the converter runs with: no secrets,
// no operator PATH entries, and an HOME that is either the private conversion
// directory or the system temporary directory, so no user-level Pandoc
// configuration, data directory or filter can be loaded.
func converterEnv(home string) []string {
	if home == "" {
		home = os.TempDir()
	}
	return []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + home,
		"LANG=C.UTF-8",
	}
}

// MaxDocumentBytes re-exports the campaign document limit so callers can check
// an upload before reading it into memory.
const MaxDocumentBytes = 10 << 20

// limitedBuffer keeps at most limit bytes and records whether the writer
// overflowed. An overflowing writer never fails the write: the caller decides
// whether the overflow is fatal, so the converter is always reaped cleanly.
type limitedBuffer struct {
	bytes.Buffer
	limit      int64
	overflowed bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.overflowed {
		return len(p), nil
	}
	remaining := b.limit - int64(b.Len())
	if remaining <= 0 {
		b.overflowed = true
		b.Reset()
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		n, _ := b.Buffer.Write(p[:remaining])
		b.overflowed = true
		return n + len(p) - int(n), nil
	}
	return b.Buffer.Write(p)
}

// Inspection describes document content that the supported rendering contract
// does not cover. Warnings are shown to the operator before approval; a
// refusal means the document is not accepted at all.
type Inspection struct {
	// Warnings name unsupported features that are dropped from the converted
	// body. They never block conversion.
	Warnings []string
	// Err is set when the document cannot be used at all.
	Err error
}

var errNotDocxPackage = fmt.Errorf("%w: the file is not a DOCX package", ErrNotDocx)

// InspectDocument classifies an uploaded document without converting it: it
// requires a DOCX package, refuses macro-enabled documents, and reports
// embedded media and tables as unsupported content. Unsupported content is
// never silently promised: no image is delivered and no attachment is added.
func InspectDocument(document []byte) Inspection {
	archive, err := zip.NewReader(bytes.NewReader(document), int64(len(document)))
	if err != nil {
		return Inspection{Err: errNotDocxPackage}
	}
	var hasDocument bool
	warnings := map[string]bool{}
	for _, file := range archive.File {
		name := file.Name
		switch {
		case name == "word/document.xml":
			hasDocument = true
			content, err := readZipFile(file, 4<<20)
			if err != nil {
				return Inspection{Err: fmt.Errorf("%w: %v", ErrNotDocx, err)}
			}
			if bytes.Contains(content, []byte("<w:tbl")) {
				warnings["tables are converted as plain text without their layout"] = true
			}
			if bytes.Contains(content, []byte("<w:drawing")) || bytes.Contains(content, []byte("<w:pict")) {
				warnings["embedded images are not delivered"] = true
			}
		case name == "word/vbaProject.bin":
			return Inspection{Err: fmt.Errorf("%w: macro-enabled documents are refused", ErrNotAllowed)}
		case strings.HasPrefix(name, "word/media/"):
			warnings["embedded images are not delivered"] = true
		case name == "word/header1.xml" || name == "word/footer1.xml":
			warnings["page headers and footers are not part of the email body"] = true
		}
	}
	if !hasDocument {
		return Inspection{Err: errNotDocxPackage}
	}
	inspection := Inspection{}
	for warning := range warnings {
		inspection.Warnings = append(inspection.Warnings, warning)
	}
	// Stable order for tests and for the operator.
	slices.Sort(inspection.Warnings)
	return inspection
}

func readZipFile(file *zip.File, limit int64) (content []byte, err error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	content, err = io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, err
	}
	return content, nil
}
