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
// is ErrUnrepresentable. So is one holding "<!--": the directives mark expands
// from the raw text, Include and Macro, start with it, and a header is no
// place for one. Every other value reads back exactly, and the line
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
	case strings.Contains(value, commentOpen):
		return "", fmt.Errorf("%s %q opens a comment, which mark reads as a directive, and %w", name, value, ErrUnrepresentable)
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
// would end the comment ("-->", "--!>") has a space put before it, a comment
// opener ("<!--") has a space put after its "<", and the space at either end
// is trimmed. These show: a title "A --> B" is published as "A -- > B", and
// "<!-- x" as "< !-- x". A value Header already accepts is returned as it is,
// so applying it twice changes nothing more.
func SanitizeValue(value string) string {
	value = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(value)
	value = strings.ReplaceAll(value, commentOpen, "< !--")
	value = strings.ReplaceAll(value, "-->", "-- >")
	value = strings.ReplaceAll(value, "--!>", "--! >")

	return strings.TrimSpace(value)
}

// commentOpen starts an HTML comment, and with it every directive mark finds
// in the raw text before it parses the document.
const commentOpen = "<!--"

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
// more than 64 deep, one with more than 100000 values in all, one that holds
// itself, or a string holding "<!--", which opens a directive.
//
// A value has to be of a kind its key holds, or it is ErrUnrepresentable
// rather than written as something mark reads back differently or leaves
// out: a string for space, title,
// type, layout, sidebar, emoji and image-align; a string or a list of strings
// for parents, folders, attachments and labels; a map with string keys for
// properties. How mark then reads each key is as for any front matter:
// strings are trimmed, empty list items left out, order has to be a whole
// number and synchronized true or false.
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

		if err := checkFrontMatterKind(normalised, fields[key]); err != nil {
			return "", fmt.Errorf("front matter key %q: %w: %w", key, ErrUnrepresentable, err)
		}
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

	node, err := (&yamlWalk{}).node(reflect.ValueOf(value), 0)
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(node)
}

// maxYAMLDepth bounds how deeply a value may nest, which is also what stops a
// value that holds itself: the recursion would otherwise overflow the stack,
// which no recover catches.
const maxYAMLDepth = 64

// maxYAMLValues bounds how many values a front matter may hold in all. The
// depth alone does not bound the work: a list that holds the same list twice,
// at each of 64 levels, is 2^64 values to write.
const maxYAMLValues = 100000

// indirect is v with its pointers and interfaces followed, each one level of
// depth, or ok false when that goes deeper than maxYAMLDepth: a pointer to an
// interface holding that pointer never ends.
func indirect(v reflect.Value, depth int) (reflect.Value, int, bool) {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) && !v.IsNil() {
		if depth >= maxYAMLDepth {
			return v, depth, false
		}
		v = v.Elem()
		depth++
	}

	return v, depth, true
}

// yamlWalk builds a value's YAML node, counting the values it has written.
type yamlWalk struct {
	values int
}

// node builds the node for v, so that each string's style is this package's
// to choose. Only what front matter is made of is taken: nil, strings,
// booleans, numbers, and slices, arrays and maps of them. A struct is refused
// rather than encoded field by field, since it would read back as a map in
// another order.
func (w *yamlWalk) node(v reflect.Value, depth int) (*yaml.Node, error) {
	v, depth, ok := indirect(v, depth)
	if !ok || depth > maxYAMLDepth {
		return nil, fmt.Errorf("a value nests more than %d deep", maxYAMLDepth)
	}
	if w.values++; w.values > maxYAMLValues {
		return nil, fmt.Errorf("front matter holds more than %d values", maxYAMLValues)
	}

	switch v.Kind() {
	case reflect.Invalid, reflect.Interface, reflect.Pointer:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil

	case reflect.String:
		if strings.Contains(v.String(), commentOpen) {
			return nil, fmt.Errorf("%q opens a comment, which mark reads as a directive", v.String())
		}
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
			item, err := w.node(v.Index(i), depth+1)
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
			keyNode, err := w.node(key, depth+1)
			if err != nil {
				return nil, err
			}
			value, err := w.node(v.MapIndex(key), depth+1)
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

// checkFrontMatterKind refuses a value its key cannot hold. ExtractMeta reads
// a string key that is not a string as unset, and leaves out a list item that
// is not a string or a property whose key is not one, so a value of the
// wrong kind would publish as something other than what was given. nil is
// any key unset.
func checkFrontMatterKind(key string, value any) error {
	v, depth, ok := indirect(reflect.ValueOf(value), 0)
	if !ok {
		return fmt.Errorf("a value nests more than %d deep", maxYAMLDepth)
	}
	if !v.IsValid() || (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		return nil
	}

	switch key {
	case "space", "title", "type", "layout", "sidebar", "emoji", "imagealign":
		if v.Kind() != reflect.String {
			return fmt.Errorf("takes a string, not a %s", v.Type())
		}

	case "parents", "folders", "attachments", "labels":
		if v.Kind() == reflect.String {
			return nil
		}
		if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
			return fmt.Errorf("takes a string or a list of strings, not a %s", v.Type())
		}
		for i := range v.Len() {
			item, _, ok := indirect(v.Index(i), depth+1)
			if !ok || item.Kind() != reflect.String {
				return fmt.Errorf("takes a list of strings, and item %d is not one", i)
			}
		}

	case "properties":
		if v.Kind() != reflect.Map {
			return fmt.Errorf("takes a map, not a %s", v.Type())
		}
		for _, k := range v.MapKeys() {
			name, _, ok := indirect(k, depth+1)
			if !ok || name.Kind() != reflect.String {
				return fmt.Errorf("takes a map with string keys, and %v is not one", k.Interface())
			}
		}
	}

	return nil
}
