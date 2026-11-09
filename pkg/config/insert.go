package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// insertMissing adds a missing key to the config file without disturbing any
// existing bytes: comments, blank lines, indentation, key quoting and the
// spelling of untouched values stay exactly the same. go-toml's unstable
// parser locates the insertion point, go-toml renders the new key, and the
// result is validated before being written atomically.
func (c *Config) insertMissing(keys []string, value any) error {
	raw, err := os.ReadFile(c.filePath)
	if err != nil {
		return err
	}

	normalized, err := toTOMLValue(value)
	if err != nil {
		return err
	}

	content, err := insertKey(raw, keys, normalized)
	if err != nil {
		return err
	}
	if err := validateEdit(content, keys); err != nil {
		return err
	}
	return writeFileAtomic(c.filePath, content)
}

// toTOMLValue converts value into the generic TOML types used for rendering.
// The round-trip keeps ReadEntry's type behavior: structs become tables,
// arrays of structs become arrays of tables, times stay datetimes.
// time.Duration is rendered as its human readable string form ("2m0s").
func toTOMLValue(value any) (any, error) {
	if d, ok := value.(time.Duration); ok {
		return d.String(), nil
	}

	data, err := toml.Marshal(map[string]any{"value": value})
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := toml.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return decoded["value"], nil
}

// insertKey splices a new key into src. keys is the dotted path; value is the
// already normalized TOML value.
func insertKey(src []byte, keys []string, value any) ([]byte, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("empty config key")
	}
	leaf := keys[len(keys)-1]
	tablePath := keys[:len(keys)-1]
	eol := detectEOL(src)

	doc, err := scanDocument(src)
	if err != nil {
		return nil, err
	}

	if len(tablePath) == 0 {
		rendered, err := renderMap(map[string]any{leaf: wrapMultilineStrings(value)})
		if err != nil {
			return nil, err
		}
		rendered = applyEOL(rendered, eol)

		if isArrayOfTables(value) {
			return appendBlock(src, rendered, eol), nil
		}
		if doc.rootLineEnd >= 0 {
			return insertLine(src, doc.rootLineEnd, rendered, eol), nil
		}
		if doc.firstHeader >= 0 {
			return insertBeforeSection(src, doc.firstHeader, rendered, eol), nil
		}
		return appendBlock(src, rendered, eol), nil
	}

	if isArrayOfTables(value) {
		return nil, fmt.Errorf("array-of-tables values are only supported at the root (key %q)", strings.Join(keys, "."))
	}

	// Append to the deepest table that already exists, writing the remaining
	// path as a dotted key. Without an ancestor, start a new top-level table.
	prefixLen := 0
	for i := len(tablePath); i > 0; i-- {
		if _, ok := doc.sections[pathKey(tablePath[:i])]; ok {
			prefixLen = i
			break
		}
	}

	if prefixLen == 0 {
		line, err := renderDotted(keys[1:], value)
		if err != nil {
			return nil, err
		}
		header := "[" + formatKeyPath(tablePath[:1]) + "]"
		return appendBlock(src, header+eol+applyEOL(line, eol), eol), nil
	}

	line, err := renderDotted(keys[prefixLen:], value)
	if err != nil {
		return nil, err
	}
	at := doc.sections[pathKey(tablePath[:prefixLen])]
	return insertLine(src, at, applyEOL(line, eol), eol), nil
}

// newEncoder returns a go-toml encoder configured for our config layout:
// two-space table indentation, one array element per line, and support for
// unstable.RawMessage so strings can be emitted as multiline strings.
func newEncoder(w io.Writer) *toml.Encoder {
	enc := toml.NewEncoder(w)
	enc.SetIndentSymbol("  ")
	enc.SetIndentTables(true)
	enc.SetArraysMultiline(true)
	enc.EnableMarshalerInterface()
	return enc
}

func renderMap(m map[string]any) (string, error) {
	var buf bytes.Buffer
	if err := newEncoder(&buf).Encode(m); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// renderDotted renders one indented key line, writing the remaining path in
// dotted-key notation (e.g. "  b.c.d = 'value'").
func renderDotted(path []string, value any) (string, error) {
	const sentinel = "__ceres_value__"

	body, err := renderTableBody(sentinel, value)
	if err != nil {
		return "", err
	}
	val, ok := strings.CutPrefix(body, "  "+sentinel+" = ")
	if !ok {
		return "", fmt.Errorf("unexpected encoded value for key %q", formatKeyPath(path))
	}
	return "  " + formatKeyPath(path) + " = " + val, nil
}

// renderTableBody renders one key indented for an existing table section.
// go-toml only indents keys below a header, so a throwaway header is rendered
// and stripped again.
func renderTableBody(leaf string, value any) (string, error) {
	const dummy = "__ceres_config_body__"

	out, err := renderMap(map[string]any{
		dummy: map[string]any{leaf: wrapMultilineStrings(value)},
	})
	if err != nil {
		return "", err
	}
	body, ok := strings.CutPrefix(out, "["+dummy+"]\n")
	if !ok {
		return "", fmt.Errorf("unexpected encoded table body for key %q", leaf)
	}
	return body, nil
}

// wrapMultilineStrings replaces every string in v with an unstable.RawMessage
// so go-toml emits it as a multiline string when it contains a newline. The
// actual TOML formatting/escaping is done by go-toml's own encoder.
func wrapMultilineStrings(v any) any {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rawString(rv.String())
	case reflect.Slice:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = wrapMultilineStrings(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = wrapMultilineStrings(iter.Value().Interface())
		}
		return out
	default:
		return v
	}
}

// rawString renders a single string value with go-toml, using the multiline
// option, and returns the raw TOML value (without the key prefix).
func rawString(s string) unstable.RawMessage {
	var buf bytes.Buffer
	// Encode can only fail for unsupported types; a plain string is always
	// encodable, so the error is intentionally ignored here.
	_ = toml.NewEncoder(&buf).Encode(struct {
		Value string `toml:"value,multiline"`
	}{Value: s})

	out, _ := strings.CutPrefix(buf.String(), "value = ")
	out, _ = strings.CutSuffix(out, "\n")
	return unstable.RawMessage(out)
}

func isArrayOfTables(v any) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice || rv.Len() == 0 {
		return false
	}
	_, ok := rv.Index(0).Interface().(map[string]any)
	return ok
}

// applyEOL converts rendered newlines to the file's line endings. Fragments
// containing multiline strings are left as-is so the string value keeps its
// literal newlines instead of turning them into CRLF.
func applyEOL(text, eol string) string {
	if eol != "\r\n" || strings.Contains(text, `"""`) {
		return text
	}
	return strings.ReplaceAll(text, "\n", eol)
}

// documentInfo describes the insertion points of a TOML document. sections maps
// a table's dotted path to the offset just past its last line (the header when
// the table has no keys).
type documentInfo struct {
	firstHeader int
	rootLineEnd int
	sections    map[string]int
}

func pathKey(path []string) string {
	// Leading NUL guarantees a non-empty key even for paths containing "".
	return "\x00" + strings.Join(path, "\x00")
}

// scanDocument walks src and records table sections and the line offsets needed
// to append keys to them.
func scanDocument(src []byte) (*documentInfo, error) {
	doc := &documentInfo{
		firstHeader: -1,
		rootLineEnd: -1,
		sections:    make(map[string]int),
	}

	parser := &unstable.Parser{KeepComments: true}
	parser.Reset(src)

	var currentKey string
	for parser.NextExpression() {
		expr := parser.Expression()
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			path, start := tablePath(src, expr)
			if doc.firstHeader < 0 {
				doc.firstHeader = start
			}
			currentKey = pathKey(path)
			doc.sections[currentKey] = lineEnd(src, start)
		case unstable.KeyValue:
			end := lineEnd(src, int(expr.Raw.Offset+expr.Raw.Length))
			if currentKey == "" {
				doc.rootLineEnd = end
				continue
			}
			doc.sections[currentKey] = end
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	return doc, nil
}

// tablePath returns the decoded dotted key of a Table/ArrayTable expression and
// the offset of the '[' that opens its header.
func tablePath(src []byte, expr *unstable.Node) ([]string, int) {
	iter := expr.Key()
	if !iter.Next() {
		return nil, 0
	}
	first := iter.Node()
	path := []string{string(first.Data)}
	for iter.Next() {
		path = append(path, string(iter.Node().Data))
	}

	keyOffset := int(first.Raw.Offset)
	start := bytes.LastIndexByte(src[:keyOffset], '\n') + 1
	for start < keyOffset && (src[start] == ' ' || src[start] == '\t') {
		start++
	}
	return path, start
}

// lineEnd returns the offset just past the newline that terminates the line
// containing pos, or len(src) when there is no newline.
func lineEnd(src []byte, pos int) int {
	if pos < len(src) {
		if idx := bytes.IndexByte(src[pos:], '\n'); idx >= 0 {
			return pos + idx + 1
		}
	}
	return len(src)
}

// spliceAtLine inserts text at at, making sure it starts on a fresh line.
func spliceAtLine(src []byte, at int, text, eol string) []byte {
	if at > 0 && src[at-1] != '\n' {
		text = eol + text
	}
	return slices.Concat(src[:at], []byte(text), src[at:])
}

// insertLine inserts line followed by eol at at.
func insertLine(src []byte, at int, line, eol string) []byte {
	return spliceAtLine(src, at, line+eol, eol)
}

// insertBeforeSection inserts a root key in front of the first table header,
// keeping a blank line between the key and the section.
func insertBeforeSection(src []byte, at int, rendered, eol string) []byte {
	return spliceAtLine(src, at, rendered+eol+eol, eol)
}

// appendBlock appends text at the end of the document, separated from existing
// content by a blank line.
func appendBlock(src []byte, text, eol string) []byte {
	out := append([]byte(nil), src...)
	if len(out) > 0 {
		out = ensureSuffix(out, "\n", eol)
		out = ensureSuffix(out, eol+eol, eol)
	}
	out = append(out, []byte(text)...)
	return ensureSuffix(out, "\n", eol)
}

func ensureSuffix(b []byte, suffix, add string) []byte {
	if bytes.HasSuffix(b, []byte(suffix)) {
		return b
	}
	return append(b, add...)
}

func detectEOL(src []byte) string {
	if bytes.Contains(src, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func formatKeyPath(parts []string) string {
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = formatKeyPart(part)
	}
	return strings.Join(out, ".")
}

func formatKeyPart(key string) string {
	if key == "" {
		return `""`
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		bare := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_'
		if !bare {
			return strconv.Quote(key)
		}
	}
	return key
}

// validateEdit decodes the edited document before it is written, so a bug in
// the insertion logic can never corrupt the config file.
func validateEdit(content []byte, keys []string) error {
	var check map[string]any
	if err := toml.Unmarshal(content, &check); err != nil {
		return fmt.Errorf("edited config would be invalid: %w", err)
	}
	_, found, err := lookupValue(check, keys)
	if err != nil {
		return fmt.Errorf("edited config is invalid for key %q: %w", strings.Join(keys, "."), err)
	}
	if !found {
		return fmt.Errorf("edited config does not contain key %q", strings.Join(keys, "."))
	}
	return nil
}

// writeFileAtomic writes the content atomically (temp file + rename) and
// preserves the file mode.
func writeFileAtomic(path string, content []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
