package convert

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"testing"
)

type closeErrorReader struct {
	io.Reader
	err error
}

func (r closeErrorReader) Close() error { return r.err }

func TestReadZipFileReportsCloseFailure(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "document.xml", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, "document content"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("decompressor close failed")
	reader.RegisterDecompressor(zip.Store, func(r io.Reader) io.ReadCloser {
		return closeErrorReader{Reader: r, err: closeErr}
	})
	if _, err := readZipFile(reader.File[0], 100); !errors.Is(err, closeErr) {
		t.Fatalf("readZipFile error = %v, want close failure", err)
	}
}
