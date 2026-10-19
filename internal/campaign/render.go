package campaign

import (
	"bytes"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"maps"
	"regexp"
	"slices"
	"strings"
	texttemplate "text/template"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Placeholder syntax is plain literal double-brace text in the subject or in
// the body text: {{variable_name}}. Markers are compiled into standard
// text/template (subject) and html/template (body) actions, so a value is
// inserted once as inert data and can never be re-evaluated as template code.
var (
	tokenPattern       = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.\- ]*?)\s*\}\}`)
	actionPattern      = regexp.MustCompile(`\{\{\.?([^}"]*)\}\}`)
	sentinelPattern    = regexp.MustCompile("\x02([^\x03]*)\x03")
	variableKeyPattern = regexp.MustCompile(`^v[0-9]+$`)
)

const (
	// sentinelOpen/sentinelClose wrap a compiled placeholder while the DOM is
	// serialized, so HTML escaping cannot damage it. literalOpen stands in for
	// a literal "{{" that is not a placeholder.
	sentinelOpen  = "\x02"
	sentinelClose = "\x03"
	// literalSentinel stands in for a literal "{{" while the DOM is serialized:
	// the HTML serializer escapes quotes, so the action itself cannot be
	// written into a text node and must be substituted afterwards.
	literalSentinel = "\x04"
	// literalBraceAction renders a literal "{{" through a standard template
	// without starting or terminating a compile-time action.
	literalBraceAction = `{{"{{"}}`
)

// UnknownVariableError reports placeholders without a mapping. The message
// names the remedy too, because either side of the mismatch may be the wrong
// one: the mapping table may be missing the row, or the document may use a
// name the table spells differently.
type UnknownVariableError struct{ Variables []string }

func (e *UnknownVariableError) Error() string {
	return fmt.Sprintf("no mapping for variable(s): %s. Add a mapping row for each name, or rename the placeholder in the subject or body.", strings.Join(e.Variables, ", "))
}

// UnusedVariableError reports mappings that appear in neither subject nor body.
// The message names the remedy too: the row is what the operator can drop, or
// the placeholder is what the document can gain.
type UnusedVariableError struct{ Variables []string }

func (e *UnusedVariableError) Error() string {
	return fmt.Sprintf("configured variable(s) missing from the subject and body: %s. Remove each mapping row, or add its placeholder to the subject or body.", strings.Join(e.Variables, ", "))
}

// MissingRequiredError reports the required variables a single row did not
// supply after normalization. Only the affected row is ineligible.
type MissingRequiredError struct{ Variables []string }

func (e *MissingRequiredError) Error() string {
	return fmt.Sprintf("missing required value(s): %s", strings.Join(e.Variables, ", "))
}

// SubjectControlError reports a replacement that introduced a header control
// character into the rendered subject.
type SubjectControlError struct{ Subject string }

func (e *SubjectControlError) Error() string {
	return "rendered subject contains control characters or line breaks"
}

// Value is one mapped replacement as it appears in a rendered email: the raw
// uploaded value is preserved next to the normalized value actually used.
type Value struct {
	Variable   string `json:"variable"`
	Header     string `json:"header"`
	Raw        string `json:"raw"`
	Normalized string `json:"normalized"`
}

// Rendered is one personalized email.
type Rendered struct {
	Recipient string  `json:"recipient"`
	Subject   string  `json:"subject"`
	Body      string  `json:"body"`
	Values    []Value `json:"values"`
}

// Template is a compiled configuration: literal placeholders rewritten into
// standard library templates plus the mapping table used to prepare values.
type Template struct {
	Variables []Variable
	Mappings  []Mapping

	subject *texttemplate.Template
	body    *htmltemplate.Template
	keys    map[string]string
}

// Compile validates the mapping table against the placeholders actually used,
// then compiles the subject and the converted body into standard templates.
// Every configured variable must appear in the subject or the body; a variable
// used in either place must be mapped; a variable may appear only in the
// subject. Placeholders split across adjacent Word runs inside one paragraph
// are joined first, and formatting outside a placeholder is preserved.
func Compile(subject, bodyHTML string, mappings []Mapping) (*Template, error) {
	if err := ValidateSubject(subject); err != nil {
		return nil, err
	}
	if err := ValidateMappings(mappings); err != nil {
		return nil, err
	}

	used := map[string]*Variable{}
	for _, name := range tokenNames(subject) {
		if err := ValidateVariableName(name); err != nil {
			return nil, fmt.Errorf("malformed placeholder in subject: %w", err)
		}
		markUsed(used, name, true, false)
	}

	// Body placeholders are joined and rewritten through the parsed HTML.
	bodySrc, err := rewriteBody(bodyHTML)
	if err != nil {
		return nil, err
	}
	for _, name := range sentinelNames(bodySrc) {
		markUsed(used, name, false, true)
	}

	configured := make(map[string]Mapping, len(mappings))
	for _, mapping := range mappings {
		configured[strings.TrimSpace(mapping.Variable)] = mapping
	}
	var unknown []string
	for name := range used {
		if _, ok := configured[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	if len(unknown) > 0 {
		return nil, &UnknownVariableError{Variables: unknown}
	}
	var unused []string
	for name, mapping := range configured {
		if _, ok := used[name]; !ok && !mapping.Recipient {
			unused = append(unused, name)
		}
	}
	slices.Sort(unused)
	if len(unused) > 0 {
		return nil, &UnusedVariableError{Variables: unused}
	}

	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	slices.Sort(names)

	keys := make(map[string]string, len(names))
	variables := make([]Variable, 0, len(names))
	for i, name := range names {
		key := fmt.Sprintf("v%d", i+1)
		keys[name] = key
		variables = append(variables, Variable{
			Name:    name,
			Key:     key,
			Subject: used[name].Subject,
			Body:    used[name].Body,
		})
	}

	subjectSrc := finalizeSubject(subject, keys)
	if err := checkPlaceholders(subjectSrc); err != nil {
		return nil, err
	}
	bodyFinal, err := finalizeBody(bodySrc, keys)
	if err != nil {
		return nil, err
	}
	if err := checkPlaceholders(bodyFinal); err != nil {
		return nil, err
	}

	compiledSubject, err := texttemplate.New("subject").Option("missingkey=error").Parse(subjectSrc)
	if err != nil {
		return nil, fmt.Errorf("compile subject template: %w", err)
	}
	compiledBody, err := htmltemplate.New("body").Option("missingkey=error").Parse(bodyFinal)
	if err != nil {
		return nil, fmt.Errorf("compile body template: %w", err)
	}

	return &Template{
		Variables: variables,
		Mappings:  mappings,
		subject:   compiledSubject,
		body:      compiledBody,
		keys:      keys,
	}, nil
}

// VarKeys returns the canonical internal key for every configured variable.
func (t *Template) VarKeys() map[string]string {
	keys := make(map[string]string, len(t.keys))
	maps.Copy(keys, t.keys)
	return keys
}

// Render prepares values from one row's raw cells and produces the subject and
// body. Raw values are preserved alongside the normalized replacements. A
// required value that is empty after normalization is reported as a
// MissingRequiredError; an optional value falls back to its configured
// fallback, then to an empty string. Values are inserted as inert data, so
// markup or template expressions inside a value stay literal text.
func (t *Template) Render(raw map[string]string) (Rendered, error) {
	data := make(map[string]string, len(t.keys))
	values := make([]Value, 0, len(t.Mappings))
	var recipient string
	var missing []string
	for _, mapping := range t.Mappings {
		rawValue := raw[mapping.Variable]
		normalized := Normalize(mapping.Format, rawValue)
		if normalized == "" {
			if mapping.Required {
				missing = append(missing, mapping.Variable)
			} else {
				normalized = Normalize(mapping.Format, mapping.Fallback)
			}
		}
		if mapping.Recipient && normalized != "" {
			var err error
			recipient, err = NormalizeRecipient(rawValue)
			if err != nil {
				return Rendered{}, fmt.Errorf("recipient variable %q: %w", mapping.Variable, ErrRecipientInvalid)
			}
			normalized = recipient
		}
		if key, used := t.keys[mapping.Variable]; used {
			data[key] = normalized
		}
		values = append(values, Value{
			Variable:   mapping.Variable,
			Header:     mapping.Header,
			Raw:        rawValue,
			Normalized: normalized,
		})
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return Rendered{}, &MissingRequiredError{Variables: missing}
	}

	var subject bytes.Buffer
	if err := t.subject.Execute(&subject, data); err != nil {
		return Rendered{}, fmt.Errorf("render subject: %w", err)
	}
	var body bytes.Buffer
	if err := t.body.Execute(&body, data); err != nil {
		return Rendered{}, fmt.Errorf("render body: %w", err)
	}
	rendered := Rendered{Recipient: recipient, Subject: subject.String(), Body: body.String(), Values: values}
	if controlPattern.MatchString(rendered.Subject) {
		return Rendered{}, &SubjectControlError{Subject: rendered.Subject}
	}
	return rendered, nil
}

// tokenNames returns the placeholder names of plain text, in document order.
func tokenNames(text string) []string {
	var names []string
	for _, match := range tokenPattern.FindAllStringSubmatch(text, -1) {
		names = append(names, strings.TrimSpace(match[1]))
	}
	return names
}

func markUsed(used map[string]*Variable, name string, subject, body bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	entry, ok := used[name]
	if !ok {
		entry = &Variable{Name: name}
		used[name] = entry
	}
	entry.Subject = entry.Subject || subject
	entry.Body = entry.Body || body
}

// sentinelNames recovers the variable names recorded as sentinels during the
// body rewrite, in first-appearance order.
func sentinelNames(bodySrc string) []string {
	seen := map[string]bool{}
	var names []string
	for _, match := range sentinelPattern.FindAllStringSubmatch(bodySrc, -1) {
		name := match[1]
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// checkPlaceholders reports a compiled body or subject that still contains an
// unexpected action, which would mean a literal brace sequence survived the
// escaping.
func checkPlaceholders(src string) error {
	for _, match := range actionPattern.FindAllStringSubmatch(src, -1) {
		if !variableKeyPattern.MatchString(match[1]) {
			return fmt.Errorf("unsupported template expression %q: variables are letters, digits and underscores", "{{"+match[1]+"}}")
		}
	}
	return nil
}

// finalizeSubject rewrites placeholders into template actions and escapes any
// remaining literal "{{" so it is emitted verbatim.
func finalizeSubject(subject string, keys map[string]string) string {
	var out strings.Builder
	last := 0
	for _, match := range tokenPattern.FindAllStringSubmatchIndex(subject, -1) {
		name := strings.TrimSpace(subject[match[2]:match[3]])
		key, ok := keys[name]
		if !ok {
			continue
		}
		out.WriteString(escapeLiteralBraces(subject[last:match[0]]))
		out.WriteString("{{." + key + "}}")
		last = match[1]
	}
	out.WriteString(escapeLiteralBraces(subject[last:]))
	return out.String()
}

// finalizeBody converts the serialized DOM sentinels into body template actions
// and escapes any remaining literal "{{".
func finalizeBody(bodySrc string, keys map[string]string) (string, error) {
	actions := make(map[string]string, len(keys))
	for name, key := range keys {
		actions[name] = "{{." + key + "}}"
	}
	var out strings.Builder
	last := 0
	for _, match := range sentinelPattern.FindAllStringSubmatchIndex(bodySrc, -1) {
		name := bodySrc[match[2]:match[3]]
		action, ok := actions[name]
		if !ok {
			return "", fmt.Errorf("internal error: placeholder %q is not mapped", name)
		}
		out.WriteString(finalizeBodySegment(bodySrc[last:match[0]]))
		out.WriteString(action)
		last = match[1]
	}
	out.WriteString(finalizeBodySegment(bodySrc[last:]))
	return out.String(), nil
}

// finalizeBodySegment escapes a serialized body segment: literal-brace
// sentinels become brace actions and any remaining "{{" is escaped too.
func finalizeBodySegment(segment string) string {
	return strings.ReplaceAll(escapeLiteralBraces(segment), literalSentinel, literalBraceAction)
}

// escapeDOMBraces replaces a literal "{{" with the serialization sentinel.
func escapeDOMBraces(text string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	return strings.ReplaceAll(text, "{{", literalSentinel)
}

// escapeLiteralBraces replaces a literal "{{" with an action that emits it, so
// a document may contain braces without becoming a template.
func escapeLiteralBraces(text string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	return strings.ReplaceAll(text, "{{", literalBraceAction)
}

// rewriteBody joins the text runs inside each block element, replaces complete
// placeholders with sentinels and escapes literal braces in text and in
// attribute values. Placeholders inside attributes are not substituted: they
// are emitted as literal text.
func rewriteBody(bodyHTML string) (string, error) {
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(bodyHTML), context)
	if err != nil {
		return "", fmt.Errorf("parse converted body: %w", err)
	}
	state := &rewriteState{indexByName: map[string]string{}}
	for _, node := range nodes {
		if err := state.walk(node); err != nil {
			return "", err
		}
	}
	var out bytes.Buffer
	for _, node := range nodes {
		if err := html.Render(&out, node); err != nil {
			return "", fmt.Errorf("serialize body: %w", err)
		}
	}
	return out.String(), nil
}

type rewriteState struct {
	indexByName map[string]string
}

// walk escapes literal braces and dispatches block elements to the run join.
func (s *rewriteState) walk(node *html.Node) error {
	switch node.Type {
	case html.TextNode:
		node.Data = escapeDOMBraces(node.Data)
		return nil
	case html.ElementNode:
		for i := range node.Attr {
			node.Attr[i].Val = escapeDOMBraces(node.Attr[i].Val)
		}
		if isBlockElement(node.Data) {
			return s.rewriteBlock(node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := s.walk(child); err != nil {
				return err
			}
		}
		return nil
	case html.DocumentNode:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := s.walk(child); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

// chunk is one text node inside a block run.
type chunk struct {
	node   *html.Node
	offset int
	length int
}

// rewriteBlock collects the inline text of one block element and replaces the
// complete placeholders found in the joined text. A placeholder split across
// adjacent runs is replaced at its first run; formatting outside the
// placeholder survives, including the inline elements that carried part of the
// placeholder.
func (s *rewriteState) rewriteBlock(block *html.Node) error {
	var chunks []chunk
	var text strings.Builder
	var err error
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if err != nil {
			return
		}
		switch node.Type {
		case html.TextNode:
			if node.Data == "" {
				return
			}
			chunks = append(chunks, chunk{node: node, offset: text.Len(), length: len(node.Data)})
			text.WriteString(node.Data)
		case html.ElementNode:
			for i := range node.Attr {
				node.Attr[i].Val = escapeDOMBraces(node.Attr[i].Val)
			}
			if node != block && isBlockElement(node.Data) {
				err = s.rewriteBlock(node)
				return
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				collect(child)
			}
		}
	}
	for child := block.FirstChild; child != nil; child = child.NextSibling {
		collect(child)
	}
	if err != nil {
		return err
	}

	joined := text.String()
	matches := tokenPattern.FindAllStringSubmatchIndex(joined, -1)
	// Edits run from the end of the join backwards so earlier offsets stay valid.
	for _, match := range slices.Backward(matches) {
		name := strings.TrimSpace(joined[match[2]:match[3]])
		if err := ValidateVariableName(name); err != nil {
			return fmt.Errorf("malformed placeholder %q: variables are letters, digits and underscores", joined[match[0]:match[1]])
		}
		sentinel, ok := s.indexByName[name]
		if !ok {
			sentinel = sentinelOpen + name + sentinelClose
			s.indexByName[name] = sentinel
		}
		if err := replaceSpan(chunks, match[0], match[1], sentinel); err != nil {
			return err
		}
	}
	for _, c := range chunks {
		c.node.Data = escapeDOMBraces(c.node.Data)
	}
	return nil
}

// replaceSpan replaces the joined-text range [start, end) with replacement,
// writing into the first affected text node and clearing the remainder.
func replaceSpan(chunks []chunk, start, end int, replacement string) error {
	first, last := -1, -1
	for i, c := range chunks {
		if first == -1 && start < c.offset+c.length {
			first = i
		}
		if c.offset < end {
			last = i
		}
	}
	if first == -1 || last == -1 || last < first {
		return errors.New("placeholder could not be located in the paragraph text")
	}
	firstOffset := start - chunks[first].offset
	lastEnd := min(end-chunks[last].offset, chunks[last].length)
	if first == last {
		data := chunks[first].node.Data
		chunks[first].node.Data = data[:firstOffset] + replacement + data[lastEnd:]
		return nil
	}
	head := chunks[first].node.Data
	chunks[first].node.Data = head[:firstOffset] + replacement
	for i := first + 1; i < last; i++ {
		chunks[i].node.Data = ""
	}
	tail := chunks[last].node.Data
	chunks[last].node.Data = tail[lastEnd:]
	return nil
}

// isBlockElement reports whether the element ends the current inline run.
func isBlockElement(tag string) bool {
	switch tag {
	case "address", "article", "aside", "blockquote", "br", "caption", "dd", "details", "div", "dl",
		"dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5",
		"h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "summary", "table",
		"tbody", "td", "tfoot", "th", "thead", "tr", "ul":
		return true
	}
	return false
}
