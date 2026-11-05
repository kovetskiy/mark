package export

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v17/metadata"
	"go.yaml.in/yaml/v3"
)

// Metadata is how a mark document spells the metadata that names its page.
// The zero value is MetadataComments.
type Metadata string

// How a mark document spells its metadata.
const (
	// MetadataComments writes the metadata as HTML comment headers, which
	// mark reads whatever features it publishes with.
	MetadataComments Metadata = "comments"

	// MetadataFrontMatter writes it as YAML front matter, which mark reads
	// only with the frontmatter feature.
	MetadataFrontMatter Metadata = "frontmatter"
)

// ParseMetadata reads the value given to --metadata.
func ParseMetadata(value string) (Metadata, error) {
	switch Metadata(strings.ToLower(strings.TrimSpace(value))) {
	case "", MetadataComments:
		return MetadataComments, nil
	case MetadataFrontMatter:
		return MetadataFrontMatter, nil
	default:
		return "", fmt.Errorf("unknown export metadata %q: expected %s or %s", value, MetadataComments, MetadataFrontMatter)
	}
}

// parseOptions reads the format and the metadata. OKF writes front matter of
// its own and has no use for the metadata, which is still checked, so that a
// configuration file setting it for mark's documents serves OKF exports too.
func parseOptions(cfg Config) (format Format, spelling Metadata, err error) {
	format, err = ParseFormat(string(cfg.Format))
	if err != nil {
		return "", "", err
	}
	spelling, err = ParseMetadata(string(cfg.Metadata))
	if err != nil {
		return "", "", err
	}
	return format, spelling, nil
}

// pageHeaders is the metadata that publishes a document back to its page.
type pageHeaders struct {
	Space             string
	Type              string
	Parents           []string
	Folders           []string
	Title             string
	Labels            []string
	Sidebar           string
	Layout            string
	ContentAppearance string
	Emoji             string
	Attachments       []string
}

// headerField is one piece of the metadata under both its names: the comment
// header, and the front matter key, which is plural where the header can be
// given more than once.
type headerField struct {
	header, key string
	list        bool
	values      []string
}

// fields are the headers in the order they are written either way.
func (h *pageHeaders) fields() []headerField {
	one := func(header, key, value string) headerField {
		return headerField{header: header, key: key, values: []string{value}}
	}
	many := func(header, key string, values []string) headerField {
		return headerField{header: header, key: key, list: true, values: values}
	}
	return []headerField{
		one(metadata.HeaderSpace, "space", h.Space),
		one(metadata.HeaderType, "type", h.Type),
		many(metadata.HeaderParent, "parents", h.Parents),
		many(metadata.HeaderFolder, "folders", h.Folders),
		one(metadata.HeaderTitle, "title", h.Title),
		many(metadata.HeaderLabel, "labels", h.Labels),
		one(metadata.HeaderSidebar, "sidebar", h.Sidebar),
		one(metadata.HeaderLayout, "layout", h.Layout),
		one(metadata.ContentAppearance, "content-appearance", h.ContentAppearance),
		one(metadata.HeaderEmoji, "emoji", h.Emoji),
		many(metadata.HeaderAttachment, "attachments", h.Attachments),
	}
}

// present is the field's values that say something, without the space around
// each, as mark reads them.
func (f headerField) present() []string {
	var values []string
	for _, value := range f.values {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// comments writes the headers as HTML comments.
func (h *pageHeaders) comments() (string, error) {
	var lines []string
	for _, field := range h.fields() {
		for _, value := range field.present() {
			value, err := headerValue(field.header, value)
			if err != nil {
				return "", err
			}
			lines = append(lines, "<!-- "+field.header+": "+value+" -->")
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// frontMatter writes the headers as YAML front matter.
func (h *pageHeaders) frontMatter() (string, error) {
	var w yamlWriter
	for _, field := range h.fields() {
		values := field.present()
		if len(values) == 0 {
			continue
		}
		if field.list {
			w.list(field.key, values)
		} else {
			w.scalar(field.key+": ", values[0])
		}
	}
	return w.document()
}

// yamlWriter writes YAML front matter one line at a time, every value by
// yamlScalar, and keeps the first error.
type yamlWriter struct {
	b   strings.Builder
	err error
}

// line writes s as it is.
func (w *yamlWriter) line(s string) {
	w.b.WriteString(s + "\n")
}

// scalar writes value after prefix, which holds the indent and the key.
func (w *yamlWriter) scalar(prefix, value string) {
	if w.err != nil {
		return
	}
	var out string
	out, w.err = yamlScalar(value)
	w.line(prefix + out)
}

// optional writes value as scalar does, unless it is empty.
func (w *yamlWriter) optional(prefix, value string) {
	if value != "" {
		w.scalar(prefix, value)
	}
}

// list writes key and values as a block sequence, or nothing for no values.
func (w *yamlWriter) list(key string, values []string) {
	if len(values) == 0 {
		return
	}
	w.line(key + ":")
	for _, value := range values {
		w.scalar("  - ", value)
	}
}

// document is what was written between --- lines.
func (w *yamlWriter) document() (string, error) {
	if w.err != nil {
		return "", w.err
	}
	return "---\n" + w.b.String() + "---\n", nil
}

// yamlScalar is value as a YAML scalar on one line: as yaml.v3 writes it, or,
// where that is in double quotes or the value has a line break, quoted by
// yamlQuote, since yaml.v3 escapes every character beyond U+FFFF, emoji
// among them.
func yamlScalar(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return yamlQuote(value), nil
	}
	out, err := yaml.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("unable to write %q as front matter: %w", value, err)
	}
	scalar := strings.TrimSuffix(string(out), "\n")
	if strings.HasPrefix(scalar, `"`) {
		return yamlQuote(value), nil
	}
	return scalar, nil
}

// yamlQuote is value as a double-quoted YAML scalar, every character YAML
// counts as printable written as it is and the rest escaped. The line breaks
// YAML 1.1 knows beyond \n and \r, U+0085, U+2028 and U+2029, are escaped
// too, so that the scalar stays on one line for every reader.
func yamlQuote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case yamlPrintable(r) && r != 0x85 && r != 0x2028 && r != 0x2029:
			b.WriteRune(r)
		case r <= 0xFFFF:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			fmt.Fprintf(&b, `\U%08X`, r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// yamlPrintable reports whether YAML lets r stand in a scalar as it is: its
// c-printable set, without the byte order mark.
func yamlPrintable(r rune) bool {
	switch {
	case r >= 0x20 && r <= 0x7E, r == 0x85:
		return true
	case r >= 0xA0 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return r != 0xFEFF
	default:
		return r >= 0x10000 && r <= 0x10FFFF
	}
}
