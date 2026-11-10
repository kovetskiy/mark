package metadata

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ErrUnknownHeader is returned by Header and FrontMatter for a key that is not
// one of the metadata mark reads. Include is not one either: it is a directive
// a later pass expands, with a value grammar of its own.
var ErrUnknownHeader = errors.New("not a metadata header mark reads")

// ErrUnrepresentable is returned by Header and FrontMatter for a value the
// format has no way to spell, so that mark would read back something other
// than what was given. The value is refused rather than altered.
var ErrUnrepresentable = errors.New("cannot be written so that mark reads it back unchanged")

// Header returns the comment header that sets key to value, as one line
// without a line break: "<!-- Title: value -->".
//
// It is for a program that writes mark documents from text it does not
// control. key is one of the Header* constants or ContentAppearance, matched
// without regard to case and written as mark spells it; anything else,
// Include among them, is ErrUnknownHeader.
//
// A comment header has no escape. Its value is everything after the first
// colon up to the closing "-->", with the space around it trimmed, so a value
// that holds a line break, has space at either end, or holds "-->" or "--!>"
// (which end a comment for every HTML reader, even where mark would read on)
// is ErrUnrepresentable. Every other value reads back exactly, and the line
// is never more than the one header. What a header does with its value --
// Order is a number, Property is key=value -- is still for ExtractMeta to
// check.
//
// Headers are read only from the comments a document opens with, so the
// lines Header returns go at the top, one per line, before anything else.
func Header(key, value string) (string, error) {
	name, ok := canonicalHeader(key)
	if !ok || name == HeaderInclude || key != strings.TrimSpace(key) {
		return "", fmt.Errorf("header %q: %w", key, ErrUnknownHeader)
	}

	switch {
	case strings.ContainsAny(value, "\r\n"):
		return "", fmt.Errorf("%s %q holds a line break and %w", name, value, ErrUnrepresentable)
	case strings.Contains(value, "-->"), strings.Contains(value, "--!>"):
		return "", fmt.Errorf("%s %q would end the comment early and %w", name, value, ErrUnrepresentable)
	case strings.TrimSpace(value) != value:
		return "", fmt.Errorf("%s %q has space at an end, which mark trims, and %w", name, value, ErrUnrepresentable)
	}

	line := "<!-- " + name + ": " + value + " -->"
	if value == "" {
		line = "<!-- " + name + ": -->"
	}

	// The line break and comment-end rules above are goldmark's and every
	// HTML reader's; the trimming and the colon cut are parseHeaderComment's,
	// checked against it so that the two cannot drift apart unnoticed.
	if k, v, ok := parseHeaderComment(line); !ok || k != name || v != value {
		return "", fmt.Errorf("%s %q: %w", name, value, ErrUnrepresentable)
	}

	return line, nil
}

// SanitizeValue is value altered just enough that Header accepts it, for a
// program that would rather publish a title close to the one it was given
// than fail over it. Header stays strict; this is the lossy alternative.
//
// Each line break -- "\r\n", "\r" or "\n" -- becomes a space, a ">" that
// would end the comment ("-->", "--!>") has a space put before it, and the
// space at either end is trimmed. A value Header already accepts is returned
// as it is, so applying it twice changes nothing more.
func SanitizeValue(value string) string {
	value = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(value)
	value = strings.ReplaceAll(value, "-->", "-- >")
	value = strings.ReplaceAll(value, "--!>", "--! >")

	return strings.TrimSpace(value)
}

// frontMatterKeys are the front matter keys mark reads, as
// normaliseFrontMatterKey spells them.
func frontMatterKeys() []string {
	return []string{
		"attachments", "contentappearance", "emoji", "folders", "imagealign",
		"labels", "layout", "order", "parents", "properties", "restrictions",
		"sidebar", "space", "synchronized", "title", "type",
	}
}

// FrontMatter returns YAML front matter holding fields, from its opening
// "---" line to its closing one and the line break after it, for a document
// that mark publishes with the frontmatter feature on.
//
// It is for a program that writes mark documents from text it does not
// control. Every key has to be one mark reads -- title, labels, parents,
// image-align and the rest, spelled as in the documentation or in any case,
// with - or _ or neither -- or the result is ErrUnknownHeader; two keys mark
// reads as one, title and Title, are ErrUnrepresentable.
//
// Unlike a comment header, YAML can spell any text, so a value's line breaks,
// dashes and quotes are all kept: the front matter mark reads back decodes to
// exactly fields, and no line inside it closes it early. A value is nil, a
// string, a boolean, a number, or a slice, array or map of them; anything
// else -- a struct, a func -- is ErrUnrepresentable, as is a value nested
// more than 64 deep or one that holds itself. How mark then reads each
// key is as for any front matter: strings are trimmed, and order has to be a
// whole number.
func FrontMatter(fields map[string]any) (string, error) {
	known := frontMatterKeys()
	seen := map[string]string{}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		normalised := normaliseFrontMatterKey(key)
		if !slices.Contains(known, normalised) {
			return "", fmt.Errorf("front matter key %q: %w", key, ErrUnknownHeader)
		}
		if other, ok := seen[normalised]; ok {
			return "", fmt.Errorf("front matter keys %q and %q are one key to mark and %w", other, key, ErrUnrepresentable)
		}
		seen[normalised] = key
	}

	out, err := marshalYAML(fields)
	if err != nil {
		return "", fmt.Errorf("front matter: %w: %w", ErrUnrepresentable, err)
	}

	document := "---\n" + string(out) + "---\n"

	// Read back the way ExtractMeta reads it. YAML decodes into other types
	// than a caller encodes from -- []any for []string -- so the two are
	// compared as YAML.
	frontMatter, _, found, err := splitFrontMatter([]byte(document))
	if err != nil || !found {
		return "", fmt.Errorf("front matter: %w", ErrUnrepresentable)
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(frontMatter, &decoded); err != nil {
		return "", fmt.Errorf("front matter: %w: %w", ErrUnrepresentable, err)
	}
	again, err := marshalYAML(decoded)
	if err != nil || !bytes.Equal(again, out) || len(decoded) != len(fields) {
		return "", fmt.Errorf("front matter: a value %w", ErrUnrepresentable)
	}

	return document, nil
}

// marshalYAML is value as YAML, with every string that holds a line break in
// double quotes, where each character is spelled out: yaml.v3 writes such a
// string as a block scalar, and loses a leading line break doing so ("\n0"
// in a list fails to read back at all).
func marshalYAML(value any) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	node, err := yamlNode(reflect.ValueOf(value), 0)
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(node)
}

// maxYAMLDepth bounds how deeply a value may nest, which is also what stops a
// value that holds itself: the recursion would otherwise overflow the stack,
// which no recover catches.
const maxYAMLDepth = 64

// yamlNode builds the node for v, so that each string's style is this
// package's to choose. Only what front matter is made of is taken: nil,
// strings, booleans, numbers, and slices, arrays and maps of them. A struct
// is refused rather than encoded field by field, since it would read back as
// a map in another order.
func yamlNode(v reflect.Value, depth int) (*yaml.Node, error) {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) && !v.IsNil() {
		v = v.Elem()
		depth++
	}
	if depth > maxYAMLDepth {
		return nil, fmt.Errorf("a value nests more than %d deep", maxYAMLDepth)
	}

	switch v.Kind() {
	case reflect.Invalid, reflect.Interface, reflect.Pointer:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil

	case reflect.String:
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v.String()}
		if strings.ContainsAny(node.Value, "\r\n\u0085\u2028\u2029") {
			node.Style = yaml.DoubleQuotedStyle
		}
		return node, nil

	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		var node yaml.Node
		if err := node.Encode(v.Interface()); err != nil {
			return nil, err
		}
		return &node, nil

	case reflect.Slice, reflect.Array:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i := range v.Len() {
			item, err := yamlNode(v.Index(i), depth+1)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, item)
		}
		return node, nil

	case reflect.Map:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			return strings.Compare(fmt.Sprint(a.Interface()), fmt.Sprint(b.Interface()))
		})
		for _, key := range keys {
			keyNode, err := yamlNode(key, depth+1)
			if err != nil {
				return nil, err
			}
			value, err := yamlNode(v.MapIndex(key), depth+1)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, keyNode, value)
		}
		return node, nil

	default:
		return nil, fmt.Errorf("a %s has no form in front matter", v.Type())
	}
}
