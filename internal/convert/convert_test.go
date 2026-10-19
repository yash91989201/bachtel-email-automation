package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// buildDocx assembles a DOCX package from named parts. Entry order is sorted
// so the produced bytes are deterministic across runs.
func buildDocx(parts map[string]string) []byte {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(parts)) {
		// Cannot fail: the writer targets an in-memory buffer.
		entry, err := writer.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := entry.Write([]byte(parts[name])); err != nil {
			panic(err)
		}
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func fixtureParts() map[string]string {
	return map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
  <Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:body>
    <w:p><w:r><w:t>This is a plain paragraph.</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Candidate Brief</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>First bullet</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Second bullet</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr></w:pPr><w:r><w:t>First numbered item</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr></w:pPr><w:r><w:t>Second numbered item</w:t></w:r></w:p>
    <w:p>
      <w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Bold </w:t></w:r>
      <w:r><w:t xml:space="preserve">and </w:t></w:r>
      <w:r><w:rPr><w:i/></w:rPr><w:t xml:space="preserve">Italic </w:t></w:r>
      <w:r><w:t xml:space="preserve">then </w:t></w:r>
      <w:hyperlink r:id="rId1"><w:r><w:t>Example link</w:t></w:r></w:hyperlink>
    </w:p>
    <w:p>
      <w:r><w:rPr><w:b/></w:rPr><w:t>{{cand</w:t></w:r>
      <w:r><w:t>idate_name}}</w:t></w:r>
    </w:p>
    <w:p><w:r><w:t>{{location}}</w:t></w:r></w:p>
    <w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr>
  </w:body>
</w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
  <Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>
</Relationships>`,
		"word/styles.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr><w:outlineLvl w:val="0"/></w:pPr>
    <w:rPr><w:b/><w:sz w:val="32"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="ListParagraph">
    <w:name w:val="List Paragraph"/>
    <w:basedOn w:val="Normal"/>
    <w:qFormat/>
    <w:pPr><w:ind w:left="720"/></w:pPr>
  </w:style>
</w:styles>`,
		"word/numbering.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:multiLevelType w:val="singleLevel"/>
    <w:lvl w:ilvl="0">
      <w:start w:val="1"/>
      <w:numFmt w:val="bullet"/>
      <w:lvlText w:val="&#8226;"/>
      <w:lvlJc w:val="left"/>
      <w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr>
    </w:lvl>
  </w:abstractNum>
  <w:abstractNum w:abstractNumId="1">
    <w:multiLevelType w:val="singleLevel"/>
    <w:lvl w:ilvl="0">
      <w:start w:val="1"/>
      <w:numFmt w:val="decimal"/>
      <w:lvlText w:val="%1."/>
      <w:lvlJc w:val="left"/>
      <w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr>
    </w:lvl>
  </w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
  <w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>
</w:numbering>`,
		"word/settings.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`,
	}
}

// fixtureDOCX builds the canonical in-memory DOCX package used by the tests:
// plain text, a heading, a bulleted list, a numbered list, bold and italic
// runs, an external hyperlink, a placeholder split across two runs, and a
// placeholder in its own paragraph.
func fixtureDOCX() []byte {
	return buildDocx(fixtureParts())
}

// tableDrawingDocument replaces the fixture body with one that contains a
// table and a drawing, both unsupported by the rendering contract.
func tableDrawingDocument() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:body>
    <w:p><w:r><w:t>Before the table.</w:t></w:r></w:p>
    <w:tbl><w:tr><w:tc><w:p><w:r><w:t>Cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
    <w:p><w:r><w:drawing/></w:r></w:p>
  </w:body>
</w:document>`
}

func hasWarning(warnings []string, want string) bool {
	return slices.ContainsFunc(warnings, func(warning string) bool {
		return strings.Contains(warning, want)
	})
}

func TestInspectDocument(t *testing.T) {
	t.Run("not a zip", func(t *testing.T) {
		inspection := InspectDocument([]byte("this is not a zip archive"))
		if !errors.Is(inspection.Err, ErrNotDocx) {
			t.Fatalf("Err = %v, want ErrNotDocx", inspection.Err)
		}
	})

	t.Run("fixture package", func(t *testing.T) {
		inspection := InspectDocument(fixtureDOCX())
		if inspection.Err != nil {
			t.Fatalf("Err = %v, want nil", inspection.Err)
		}
		if len(inspection.Warnings) != 0 {
			t.Fatalf("Warnings = %v, want none", inspection.Warnings)
		}
	})

	t.Run("macro project refused", func(t *testing.T) {
		parts := fixtureParts()
		parts["word/vbaProject.bin"] = "fake macro payload"
		inspection := InspectDocument(buildDocx(parts))
		if !errors.Is(inspection.Err, ErrNotAllowed) {
			t.Fatalf("Err = %v, want ErrNotAllowed", inspection.Err)
		}
	})

	t.Run("embedded media warns", func(t *testing.T) {
		parts := fixtureParts()
		parts["word/media/image1.png"] = "fake png payload"
		inspection := InspectDocument(buildDocx(parts))
		if inspection.Err != nil {
			t.Fatalf("Err = %v, want nil", inspection.Err)
		}
		if !hasWarning(inspection.Warnings, "embedded images are not delivered") {
			t.Fatalf("Warnings = %v, want embedded image warning", inspection.Warnings)
		}
	})

	t.Run("table and drawing warn", func(t *testing.T) {
		parts := fixtureParts()
		parts["word/document.xml"] = tableDrawingDocument()
		inspection := InspectDocument(buildDocx(parts))
		if inspection.Err != nil {
			t.Fatalf("Err = %v, want nil", inspection.Err)
		}
		if !hasWarning(inspection.Warnings, "tables are converted as plain text") {
			t.Errorf("Warnings = %v, want table warning", inspection.Warnings)
		}
		if !hasWarning(inspection.Warnings, "embedded images are not delivered") {
			t.Errorf("Warnings = %v, want embedded image warning", inspection.Warnings)
		}
	})

	t.Run("missing document.xml", func(t *testing.T) {
		parts := fixtureParts()
		delete(parts, "word/document.xml")
		inspection := InspectDocument(buildDocx(parts))
		if !errors.Is(inspection.Err, ErrNotDocx) {
			t.Fatalf("Err = %v, want ErrNotDocx", inspection.Err)
		}
	})
}

func TestDOCXToHTMLWithoutPandoc(t *testing.T) {
	ctx := context.Background()

	t.Run("empty document", func(t *testing.T) {
		_, err := Converter{}.DOCXToHTML(ctx, nil)
		if !errors.Is(err, ErrNotDocx) {
			t.Fatalf("err = %v, want ErrNotDocx", err)
		}
	})

	t.Run("document above the size limit", func(t *testing.T) {
		document := bytes.Repeat([]byte("a"), MaxDocumentBytes+1)
		_, err := Converter{}.DOCXToHTML(ctx, document)
		if !errors.Is(err, ErrInputTooLarge) {
			t.Fatalf("err = %v, want ErrInputTooLarge", err)
		}
	})

	t.Run("missing executable", func(t *testing.T) {
		converter := Converter{Path: "definitely-not-a-real-pandoc-binary"}
		_, err := converter.DOCXToHTML(ctx, fixtureDOCX())
		if !errors.Is(err, ErrConverterMissing) {
			t.Fatalf("DOCXToHTML err = %v, want ErrConverterMissing", err)
		}
		_, err = converter.Version(ctx)
		if !errors.Is(err, ErrConverterMissing) {
			t.Fatalf("Version err = %v, want ErrConverterMissing", err)
		}
	})
}

// requirePandoc skips the calling test when no usable Pandoc executable is
// installed, so the package tests pass on machines without the converter.
func requirePandoc(t *testing.T) {
	t.Helper()
	if _, err := (Converter{}).Executable(); err != nil {
		t.Skipf("pandoc not installed: %v", err)
	}
}

// TestDOCXToHTMLInvocationContract pins the properties that keep the converter
// safe: no shell, exactly the fixed argument list, a private temporary
// directory holding one 0600 input file, a scrubbed environment that cannot
// carry application secrets or operator configuration, and no leftover
// temporary file. It uses a stub in place of Pandoc, so it always runs.
func TestDOCXToHTMLInvocationContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub converter is a POSIX shell script")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "pandoc")
	const script = `#!/bin/sh
here=$(dirname "$0")
printf '%s\n' "$@" > "$here/argv.txt"
env > "$here/env.txt"
last=""
for arg in "$@"; do last="$arg"; done
case "$1" in
--version) printf 'pandoc stub 1.0\n';;
*)
  if [ -f "$last" ]; then
    stat -c '%a' "$last" > "$here/input.mode"
    stat -c '%a' "$(dirname "$last")" > "$here/dir.mode"
    printf '%s' "$last" > "$here/input.path"
  fi
  printf '<p>stub body</p>\n';;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// A secret in the environment must never reach the converter process.
	t.Setenv("APP_ENCRYPTION_KEY", "super-secret-value")

	converter := Converter{Path: stub}
	html, err := converter.DOCXToHTML(context.Background(), fixtureDOCX())
	if err != nil {
		t.Fatalf("DOCXToHTML: %v", err)
	}
	if !strings.Contains(html, "stub body") {
		t.Fatalf("converted html = %q, want the stub output", html)
	}

	recorded := func(name string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(content)
	}

	inputPath := strings.TrimSpace(recorded("input.path"))
	argv := strings.Split(strings.TrimSpace(recorded("argv.txt")), "\n")
	want := []string{
		"--from=docx",
		"--to=html5",
		"--no-highlight",
		"--wrap=none",
		"--sandbox",
		"--output=-",
		inputPath,
	}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv = %q, want the fixed argument list %q", argv, want)
	}
	if !strings.HasPrefix(inputPath, os.TempDir()+string(os.PathSeparator)) || filepath.Base(inputPath) != "body.docx" {
		t.Fatalf("input path = %q, want a file named body.docx inside the system temporary directory", inputPath)
	}
	if mode := strings.TrimSpace(recorded("input.mode")); mode != "600" {
		t.Fatalf("input file mode = %q, want 600", mode)
	}
	if mode := strings.TrimSpace(recorded("dir.mode")); mode != "700" {
		t.Fatalf("conversion directory mode = %q, want 700", mode)
	}

	// The stub shell adds a few of its own bookkeeping variables (PWD, SHLVL,
	// _), so the environment is checked as a set: only the scrubbed entries and
	// those shell internals are allowed, with the fixed values.
	environment := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(recorded("env.txt")), "\n") {
		name, value, _ := strings.Cut(line, "=")
		if _, allowed := map[string]bool{"PWD": true, "HOME": true, "LANG": true, "SHLVL": true, "PATH": true, "_": true}[name]; !allowed {
			t.Fatalf("converter environment carried an unexpected variable %q", line)
		}
		environment[name] = value
	}
	for name, want := range map[string]string{
		"PATH": "/usr/local/bin:/usr/bin:/bin",
		"HOME": filepath.Dir(inputPath),
		"LANG": "C.UTF-8",
	} {
		if environment[name] != want {
			t.Fatalf("converter %s = %q, want %q", name, environment[name], want)
		}
	}
	if strings.Contains(recorded("env.txt"), "super-secret-value") {
		t.Fatal("the converter environment carried an application secret")
	}

	// The private directory is removed once the conversion is done.
	if _, err := os.Stat(filepath.Dir(inputPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("conversion directory still exists after the call: %v", err)
	}
}

var (
	reParagraph = regexp.MustCompile(`(?s)<p\b[^>]*>(.*?)</p>`)
	reTag       = regexp.MustCompile(`<[^>]*>`)
	reListUl    = regexp.MustCompile(`(?s)<ul\b[^>]*>(.*?)</ul>`)
	reListOl    = regexp.MustCompile(`(?s)<ol\b[^>]*>(.*?)</ol>`)
	reLink      = regexp.MustCompile(`(?s)<a\b([^>]*)>(.*?)</a>`)
)

// paragraphTexts returns the joined, tag-free text of every paragraph in the
// converted HTML. Joining across runs is what proves a placeholder survives a
// run split inside the DOCX.
func paragraphTexts(html string) []string {
	var texts []string
	for _, match := range reParagraph.FindAllStringSubmatch(html, -1) {
		texts = append(texts, strings.TrimSpace(reTag.ReplaceAllString(match[1], "")))
	}
	return texts
}

func listHasItem(html, kind string) bool {
	re := reListUl
	if kind == "ol" {
		re = reListOl
	}
	for _, match := range re.FindAllStringSubmatch(html, -1) {
		if strings.Contains(match[1], "<li") {
			return true
		}
	}
	return false
}

func hasLinkTo(html, href, text string) bool {
	for _, match := range reLink.FindAllStringSubmatch(html, -1) {
		if !strings.Contains(match[1], `href="`+href+`"`) {
			continue
		}
		if strings.TrimSpace(reTag.ReplaceAllString(match[2], "")) == text {
			return true
		}
	}
	return false
}

func TestDOCXToHTMLWithPandoc(t *testing.T) {
	requirePandoc(t)
	ctx := context.Background()
	fixture := fixtureDOCX()

	t.Run("fixture converts to semantic HTML", func(t *testing.T) {
		html, err := (Converter{}).DOCXToHTML(ctx, fixture)
		if err != nil {
			t.Fatalf("DOCXToHTML: %v", err)
		}

		paragraphs := paragraphTexts(html)
		if !slices.ContainsFunc(paragraphs, func(p string) bool {
			return strings.Contains(p, "{{candidate_name}}")
		}) {
			t.Errorf("no paragraph contains the joined {{candidate_name}} placeholder; paragraphs: %q", paragraphs)
		}
		if !slices.ContainsFunc(paragraphs, func(p string) bool {
			return strings.Contains(p, "{{location}}")
		}) {
			t.Errorf("no paragraph contains {{location}}; paragraphs: %q", paragraphs)
		}

		if !regexp.MustCompile(`<h1\b`).MatchString(html) {
			t.Error("no <h1> heading in output")
		}
		if !listHasItem(html, "ul") {
			t.Error("no <ul> with an <li> in output")
		}
		if !listHasItem(html, "ol") {
			t.Error("no <ol> with an <li> in output")
		}
		if !strings.Contains(html, "<strong>") {
			t.Error("no <strong> in output")
		}
		if !strings.Contains(html, "<em>") {
			t.Error("no <em> in output")
		}
		if !hasLinkTo(html, "https://example.com", "Example link") {
			t.Error("no anchor to https://example.com with text \"Example link\" in output")
		}
		if strings.Contains(html, "<img") {
			t.Error("output contains an <img> element")
		}
	})

	t.Run("timeout is reported", func(t *testing.T) {
		_, err := (Converter{Timeout: time.Nanosecond}).DOCXToHTML(ctx, fixture)
		if err == nil {
			t.Fatal("err = nil, want a timeout error")
		}
		if !strings.Contains(err.Error(), "did not finish within") {
			t.Fatalf("err = %q, want timeout message", err)
		}
	})
}
