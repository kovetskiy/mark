package export

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v16/metadata"
	"go.yaml.in/yaml/v3"
)

// How a mark document spells its metadata.
const (
	// MetadataComments writes the metadata as HTML comment headers, which
	// mark reads whatever features it publishes with.
	MetadataComments = "comments"

	// MetadataFrontMatter writes it as YAML front matter, which mark reads
	// only with the frontmatter feature.
	MetadataFrontMatter = "front-matter"
)

// ParseMetadata reads the value given to --metadata.
func ParseMetadata(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", MetadataComments:
		return MetadataComments, nil
	case MetadataFrontMatter, "frontmatter":
		return MetadataFrontMatter, nil
	default:
		return "", fmt.Errorf("unknown export metadata %q: expected %s or %s", value, MetadataComments, MetadataFrontMatter)
	}
}

// parseOptions reads the format and the metadata together, refusing the one
// pair that does not go: OKF writes front matter of its own.
func parseOptions(cfg Config) (format, spelling string, err error) {
	format, err = ParseFormat(cfg.Format)
	if err != nil {
		return "", "", err
	}
	spelling, err = ParseMetadata(cfg.Metadata)
	if err != nil {
		return "", "", err
	}
	if format == FormatOKF && spelling == MetadataFrontMatter {
		return "", "", fmt.Errorf("the %s format writes front matter of its own: %s metadata is for the %s format", FormatOKF, MetadataFrontMatter, FormatMark)
	}
	return format, spelling, nil
}

// pageHeaders is the metadata that publishes a document back to its page,
// with the front matter keys mark reads, in the order the headers are
// written.
type pageHeaders struct {
	Space             string   `yaml:"space"`
	Type              string   `yaml:"type,omitempty"`
	Parents           []string `yaml:"parents,omitempty"`
	Folders           []string `yaml:"folders,omitempty"`
	Title             string   `yaml:"title"`
	Labels            []string `yaml:"labels,omitempty"`
	Sidebar           string   `yaml:"sidebar,omitempty"`
	Layout            string   `yaml:"layout,omitempty"`
	ContentAppearance string   `yaml:"content-appearance,omitempty"`
	Emoji             string   `yaml:"emoji,omitempty"`
	Attachments       []string `yaml:"attachments,omitempty"`
}

// comments writes the headers as HTML comments.
func (h *pageHeaders) comments() (string, error) {
	var lines []string
	add := func(name string, values ...string) error {
		for _, value := range values {
			if value == "" {
				continue
			}
			value, err := headerValue(name, value)
			if err != nil {
				return err
			}
			lines = append(lines, "<!-- "+name+": "+value+" -->")
		}
		return nil
	}

	for _, header := range []struct {
		name   string
		values []string
	}{
		{metadata.HeaderSpace, []string{h.Space}},
		{metadata.HeaderType, []string{h.Type}},
		{metadata.HeaderParent, h.Parents},
		{metadata.HeaderFolder, h.Folders},
		{metadata.HeaderTitle, []string{h.Title}},
		{metadata.HeaderLabel, h.Labels},
		{metadata.HeaderSidebar, []string{h.Sidebar}},
		{metadata.HeaderLayout, []string{h.Layout}},
		{metadata.ContentAppearance, []string{h.ContentAppearance}},
		{metadata.HeaderEmoji, []string{h.Emoji}},
		{metadata.HeaderAttachment, h.Attachments},
	} {
		if err := add(header.name, header.values...); err != nil {
			return "", err
		}
	}

	return strings.Join(lines, "\n") + "\n", nil
}

// frontMatter writes the headers as YAML front matter, under the keys and in
// the order the comments are written.
func (h *pageHeaders) frontMatter() (string, error) {
	var b strings.Builder
	b.WriteString("---\n")

	for _, field := range []struct {
		key    string
		value  string
		values []string
	}{
		{key: "space", value: h.Space},
		{key: "type", value: h.Type},
		{key: "parents", values: h.Parents},
		{key: "folders", values: h.Folders},
		{key: "title", value: h.Title},
		{key: "labels", values: h.Labels},
		{key: "sidebar", value: h.Sidebar},
		{key: "layout", value: h.Layout},
		{key: "content-appearance", value: h.ContentAppearance},
		{key: "emoji", value: h.Emoji},
		{key: "attachments", values: h.Attachments},
	} {
		if field.value != "" {
			value, err := yamlScalar(field.value)
			if err != nil {
				return "", err
			}
			b.WriteString(field.key + ": " + value + "\n")
		}
		if len(field.values) == 0 {
			continue
		}
		if field.value == "" {
			b.WriteString(field.key + ":\n")
		}
		for _, item := range field.values {
			value, err := yamlScalar(item)
			if err != nil {
				return "", err
			}
			b.WriteString("  - " + value + "\n")
		}
	}

	b.WriteString("---\n")
	return b.String(), nil
}

// yamlScalar is value as a YAML scalar on one line: quoted where YAML would
// read it as something other than that string, and double-quoted, with its
// line breaks escaped, where it has any.
func yamlScalar(value string) (string, error) {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	if strings.ContainsAny(value, "\r\n") {
		node.Style = yaml.DoubleQuotedStyle
	}
	out, err := yaml.Marshal(node)
	if err != nil {
		return "", fmt.Errorf("unable to write %q as front matter: %w", value, err)
	}
	scalar := strings.TrimSuffix(string(out), "\n")
	if strings.HasPrefix(scalar, `"`) {
		scalar = unescapeAstral(scalar)
	}
	return scalar, nil
}

// unescapeAstral writes the \UXXXXXXXX escapes yaml.v3 gives every character
// beyond U+FFFF, emoji among them, as the characters, which a double-quoted
// scalar may hold as they are. It is given one double-quoted scalar, where
// every backslash begins an escape.
func unescapeAstral(scalar string) string {
	var out strings.Builder
	for i := 0; i < len(scalar); i++ {
		if scalar[i] != '\\' {
			out.WriteByte(scalar[i])
			continue
		}
		if i+10 <= len(scalar) && scalar[i+1] == 'U' {
			if r, ok := hexRune(scalar[i+2 : i+10]); ok && r > 0xFFFF {
				out.WriteRune(r)
				i += 9
				continue
			}
		}
		// Any other escape is kept whole, so that the second backslash of \\
		// is not read as beginning one.
		out.WriteByte('\\')
		if i+1 < len(scalar) {
			i++
			out.WriteByte(scalar[i])
		}
	}
	return out.String()
}

// hexRune reads the hexadecimal digits of an escape as the character they
// name, if they name one.
func hexRune(digits string) (rune, bool) {
	var r rune
	for _, c := range []byte(digits) {
		var d byte
		switch {
		case '0' <= c && c <= '9':
			d = c - '0'
		case 'a' <= c && c <= 'f':
			d = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | rune(d)
	}
	return r, utf8.ValidRune(r)
}
