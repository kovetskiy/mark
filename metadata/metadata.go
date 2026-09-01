package metadata

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	HeaderParent       = `Parent`
	HeaderFolder       = `Folder`
	HeaderSpace        = `Space`
	HeaderType         = `Type`
	HeaderTitle        = `Title`
	HeaderLayout       = `Layout`
	HeaderEmoji        = `Emoji`
	HeaderAttachment   = `Attachment`
	HeaderLabel        = `Label`
	HeaderOrder        = `Order`
	HeaderInclude      = `Include`
	HeaderSidebar      = `Sidebar`
	ContentAppearance  = `Content-Appearance`
	HeaderImageAlign   = `Image-Align`
	HeaderProperty     = `Property`
	HeaderSynchronized = `Synchronized`
)

// knownHeaders is every header name a document may write, in the order an
// unknown one is reported against.
var knownHeaders = []string{
	HeaderAttachment,
	ContentAppearance,
	HeaderEmoji,
	HeaderFolder,
	HeaderImageAlign,
	HeaderInclude,
	HeaderLabel,
	HeaderLayout,
	HeaderOrder,
	HeaderParent,
	HeaderProperty,
	HeaderSidebar,
	HeaderSpace,
	HeaderSynchronized,
	HeaderTitle,
	HeaderType,
}

type Meta struct {
	Parents           []string
	Folders           []string
	Space             string
	Type              string
	Title             string
	Layout            string
	Sidebar           string
	Emoji             string
	Attachments       []string
	Labels            []string
	ContentAppearance string

	// Synchronized is whether the document is published at all. Nil means it
	// said nothing, which is not the same as false: a pointer keeps the
	// difference between a document opting out and a Meta that simply never
	// mentioned it, so that no code path can accidentally skip a page by
	// leaving a field at its zero value.
	Synchronized *bool

	// Properties are Confluence content properties to set on the page.
	//
	// Values are whatever the document wrote, because a content property holds
	// JSON and a caller may reasonably want a number, a list or an object. The
	// Property header can only express a string; front matter can express the
	// rest.
	Properties map[string]any

	// Order positions this page among its siblings, smaller first. Nil means
	// the document said nothing about order, which is not the same as zero:
	// mark leaves such pages exactly where Confluence has them rather than
	// sorting them to the front.
	Order      *int
	ImageAlign string
}

const (
	FullWidthContentAppearance = "full-width"
	FixedContentAppearance     = "fixed"
	DefaultContentAppearance   = "default"
)

func toStringSlice(val any) []string {
	v, ok := val.([]any)
	if !ok {
		return nil
	}
	var res []string
	for _, item := range v {
		if s, ok := item.(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				res = append(res, s)
			}
		}
	}
	return res
}

// toBool reads a front matter boolean, which YAML gives as a bool but a quoted
// document gives as a string.
func toBool(val any) (bool, bool) {
	switch v := val.(type) {
	case bool:
		return v, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))

		return parsed, err == nil
	default:
		return false, false
	}
}

// toStringMap reads a front matter mapping, keeping the values as written.
//
// A YAML mapping decodes to map[string]any, but a document nested inside
// another structure can arrive as map[any]any, so both are accepted.
func toStringMap(val any) map[string]any {
	switch v := val.(type) {
	case map[string]any:
		return v
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			if name, ok := key.(string); ok {
				out[name] = value
			}
		}

		return out
	default:
		return nil
	}
}

func toString(val any) string {
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func setContentAppearance(meta *Meta, value string) {
	switch strings.TrimSpace(value) {
	case FixedContentAppearance:
		meta.ContentAppearance = FixedContentAppearance
	case DefaultContentAppearance:
		meta.ContentAppearance = DefaultContentAppearance
	default:
		meta.ContentAppearance = FullWidthContentAppearance
	}
}

func stripFrontMatter(data []byte) ([]byte, error) {
	delimiter, rest, ok := bytes.Cut(data, []byte("\n"))
	if !ok {
		return nil, fmt.Errorf("unterminated YAML front matter")
	}
	delimiter = bytes.TrimRight(delimiter, " \r\t")

	for {
		line, remaining, hasNewline := bytes.Cut(rest, []byte("\n"))
		if bytes.Equal(bytes.TrimRight(line, " \r\t"), delimiter) {
			return remaining, nil
		}
		if !hasNewline {
			return nil, fmt.Errorf("unterminated YAML front matter")
		}
		rest = remaining
	}
}

func ExtractMeta(data []byte, spaceFromCli string, titleFromH1 bool, titleFromFilename bool, filename string, parents []string, titleAppendGeneratedHash bool, defaultContentAppearance string, frontMatterEnabled bool) (*Meta, []byte, error) {
	var meta *Meta
	body := data

	markdown := goldmark.New()
	if frontMatterEnabled {
		(&frontmatter.Extender{
			Formats: []frontmatter.Format{frontmatter.YAML},
		}).Extend(markdown)
	}

	ctx := parser.NewContext()
	doc := markdown.Parser().Parse(text.NewReader(data), parser.WithContext(ctx))
	if frontMatterData := frontmatter.Get(ctx); frontMatterData != nil {
		var parsed map[string]any
		if err := frontMatterData.Decode(&parsed); err != nil {
			return nil, nil, fmt.Errorf("decode YAML front matter: %w", err)
		}

		meta = &Meta{}
		meta.Type = "page" // Default type

		for k, v := range parsed {
			normKey := strings.ToLower(k)
			normKey = strings.ReplaceAll(normKey, "-", "")
			normKey = strings.ReplaceAll(normKey, "_", "")

			switch normKey {
			case "parents":
				meta.Parents = append(meta.Parents, toStringSlice(v)...)
			case "folders":
				meta.Folders = append(meta.Folders, toStringSlice(v)...)
			case "space":
				meta.Space = toString(v)
			case "type":
				meta.Type = toString(v)
			case "title":
				meta.Title = toString(v)
			case "layout":
				meta.Layout = toString(v)
			case "sidebar":
				meta.Sidebar = toString(v)
			case "emoji":
				meta.Emoji = toString(v)
			case "attachments":
				meta.Attachments = append(meta.Attachments, toStringSlice(v)...)
			case "labels":
				meta.Labels = append(meta.Labels, toStringSlice(v)...)
			case "contentappearance":
				setContentAppearance(meta, toString(v))
			case "imagealign":
				meta.ImageAlign = strings.ToLower(toString(v))
			case "synchronized":
				value, ok := toBool(v)
				if !ok {
					return nil, nil, fmt.Errorf(
						"synchronized must be true or false, got %v", v,
					)
				}
				meta.Synchronized = &value
			case "properties":
				for key, value := range toStringMap(v) {
					if meta.Properties == nil {
						meta.Properties = map[string]any{}
					}
					meta.Properties[key] = value
				}
			}
		}

		// Presence of a non-empty sidebar forces the article layout, regardless of map key iteration order.
		if meta.Sidebar != "" {
			meta.Layout = "article"
		}

		var err error
		body, err = stripFrontMatter(data)
		if err != nil {
			return nil, nil, err
		}
	}

	// Where the run of headers begins and ends. Two boundaries rather than one
	// because a document may open with something that is not a header at all --
	// a Macro definition spanning several lines is the usual case -- and that
	// has to survive into the body rather than being swallowed as though it
	// were metadata.
	firstStart := -1

	// Where body already begins within data, which is past the front matter
	// when there was any. stripFrontMatter returns a suffix of data, so the
	// difference in lengths is the offset.
	bodyStart := len(data) - len(body)

	var lastStop int
	shouldBreak := false

	// The Include directives found among the headers. They are metadata here
	// only in that this loop has to recognise them; the expansion happens later
	// and reads the body, so each one is carried across the cut below.
	var includes []text.Segment

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		if htmlBlock, ok := child.(*ast.HTMLBlock); ok {
			lines := htmlBlock.Lines()
			if lines.Len() > 0 {
				if lastStop > 0 && !onlyWhitespace(data, lastStop, lines.At(0).Start) {
					break
				}
			}

			for i := 0; i < lines.Len(); i++ {
				lineSeg := lines.At(i)
				line := string(lineSeg.Value(data))

				key, value, ok := parseHeaderComment(line)
				if !ok {
					if firstStart == -1 {
						// Nothing has been read as a header yet, so this block
						// is not the header block: a Macro or Include directive
						// written above them, or raw HTML. Leave it alone and
						// look at the next one, rather than giving up on the
						// document's metadata entirely.
						break
					}

					shouldBreak = true
					break
				}

				if meta == nil {
					meta = &Meta{}
					meta.Type = "page" // Default if not specified
				}

				if firstStart == -1 {
					firstStart = lineSeg.Start
				}

				header := cases.Title(language.English).String(key)

				switch header {
				case HeaderParent:
					meta.Parents = append(meta.Parents, value)

				case HeaderFolder:
					meta.Folders = append(meta.Folders, value)

				case HeaderSpace:
					meta.Space = strings.TrimSpace(value)

				case HeaderType:
					meta.Type = strings.TrimSpace(value)

				case HeaderTitle:
					meta.Title = strings.TrimSpace(value)

				case HeaderLayout:
					meta.Layout = strings.TrimSpace(value)

				case HeaderSidebar:
					meta.Layout = "article"
					meta.Sidebar = strings.TrimSpace(value)

				case HeaderEmoji:
					meta.Emoji = strings.TrimSpace(value)

				case HeaderOrder:
					order, err := strconv.Atoi(strings.TrimSpace(value))
					if err != nil {
						return nil, nil, fmt.Errorf(
							"%s header must be a whole number, got %q", HeaderOrder, value,
						)
					}
					meta.Order = &order

				case HeaderAttachment:
					meta.Attachments = append(meta.Attachments, value)

				case HeaderLabel:
					meta.Labels = append(meta.Labels, value)

				case HeaderInclude:
					// Includes are parsed by a different func, which reads the
					// body -- so this line has to survive being cut out along
					// with the headers around it. Without that, an Include
					// written among the headers was deleted before anything
					// could expand it and the page went up missing a whole
					// section, with a zero exit code.
					includes = append(includes, lineSeg)
					lastStop = lineSeg.Stop
					continue

				case ContentAppearance:
					setContentAppearance(meta, value)

				case HeaderImageAlign:
					meta.ImageAlign = strings.ToLower(strings.TrimSpace(value))

				case HeaderSynchronized:
					synchronized, err := strconv.ParseBool(strings.TrimSpace(value))
					if err != nil {
						return nil, nil, fmt.Errorf(
							"%s header must be true or false, got %q",
							HeaderSynchronized, value,
						)
					}
					meta.Synchronized = &synchronized

				case HeaderProperty:
					key, propValue, ok := strings.Cut(value, "=")
					if !ok {
						return nil, nil, fmt.Errorf(
							"%s header must be written as key=value, got %q",
							HeaderProperty, value,
						)
					}

					key = strings.TrimSpace(key)
					if key == "" {
						return nil, nil, fmt.Errorf(
							"%s header has no name: %q", HeaderProperty, value,
						)
					}

					if meta.Properties == nil {
						meta.Properties = map[string]any{}
					}
					meta.Properties[key] = strings.TrimSpace(propValue)

				default:
					// Refused rather than reported. The line is inside the
					// header run, so it is taken out of the body either way,
					// and a run that logged the typo and published the page
					// without whatever the header asked for still exited zero
					// -- a misspelled Title changed what was published and only
					// said so in a line of output nobody was reading.
					return nil, nil, fmt.Errorf(
						"unknown header %q, expected one of: %s",
						header, strings.Join(knownHeaders, ", "),
					)
				}

				lastStop = lineSeg.Stop
			}

			if shouldBreak {
				break
			}
		} else {
			break
		}
	}

	if lastStop > 0 {
		// Only the headers are taken out. Whatever preceded them stays, which
		// is what keeps a Macro defined above the headers working, and the
		// Include directives found among them are put back where they stood.
		rebuilt := make([]byte, 0, (firstStart-bodyStart)+(len(data)-lastStop))
		rebuilt = append(rebuilt, data[bodyStart:firstStart]...)
		for _, include := range includes {
			rebuilt = append(rebuilt, data[include.Start:include.Stop]...)
		}
		body = append(rebuilt, data[lastStop:]...)
	}

	if meta != nil {
		warnStrandedHeaders(body)
	}

	if titleFromH1 || titleFromFilename || spaceFromCli != "" {
		if meta == nil {
			meta = &Meta{}
		}

		if meta.Type == "" {
			meta.Type = "page"
		}

		if titleFromH1 && meta.Title == "" {
			meta.Title = ExtractDocumentLeadingH1(doc, data)
		}
		if titleFromFilename && meta.Title == "" && filename != "" {
			setTitleFromFilename(meta, filename)
		}
		if spaceFromCli != "" && meta.Space == "" {
			meta.Space = spaceFromCli
		}
	}

	// Use the global content appearance flag if the header is not set in the document
	if meta != nil && defaultContentAppearance != "" && meta.ContentAppearance == "" {
		setContentAppearance(meta, defaultContentAppearance)
	} else if meta != nil && meta.ContentAppearance == "" {
		meta.ContentAppearance = FullWidthContentAppearance // Default to full-width if nothing else is set for backwards compatibility
	}

	if meta == nil {
		return nil, data, nil
	}

	// Prepend parent pages that are defined via the cli flag
	if len(parents) > 0 && parents[0] != "" {
		meta.Parents = append(parents, meta.Parents...)
	}

	// deterministically generate a hash from the page's parents, space, and title
	if titleAppendGeneratedHash {
		path := strings.Join(append(meta.Parents, meta.Space, meta.Title), "/")
		pathHash := sha256.Sum256([]byte(path))
		// postfix is an 8-character hexadecimal string representation of the first 4 out of 32 bytes of the hash
		meta.Title = fmt.Sprintf("%s - %x", meta.Title, pathHash[0:4])
		log.Debug().Msgf("appended hash to page title: %s", meta.Title)
	}

	// Remove trailing spaces from title
	meta.Title = strings.Trim(meta.Title, " ")
	meta.Space = strings.Trim(meta.Space, " ")
	return meta, body, nil
}

// onlyWhitespace reports whether nothing but whitespace separates one header
// comment from the next.
//
// Each comment is its own HTML block, and the run of them used to have to be
// byte-contiguous: a single blank line ended it, so every header below the gap
// was neither applied nor removed. Grouping headers -- identity, then labels,
// then properties -- is the natural way to write them and nothing ever said it
// was forbidden; the symptom was a Label that quietly did nothing and its own
// comment published as the first line of the page.
func onlyWhitespace(data []byte, from, to int) bool {
	if from > to || to > len(data) {
		return false
	}

	return strings.TrimSpace(string(data[from:to])) == ""
}

// warnStrandedHeaders reports header comments that stayed in the page body.
//
// Headers are read only from the run of comments the document opens with, so
// one written after a paragraph -- or after anything else that ends the run --
// is not metadata at all. It is published as page text and whatever it asked
// for silently never happens, which is the failure a blank line used to cause
// and which anything else ending the run still can. Saying so is cheap;
// guessing what was meant is not.
func warnStrandedHeaders(body []byte) {
	for _, line := range strings.Split(string(body), "\n") {
		key, _, ok := parseHeaderComment(line)
		if !ok {
			continue
		}

		header := cases.Title(language.English).String(key)
		if header == HeaderInclude {
			// An Include below the headers is the ordinary way to write one.
			continue
		}

		for _, known := range knownHeaders {
			if header == known {
				log.Warn().Msgf(
					"%s header is below the document's opening comments, so it is published as text rather than applied: %s",
					header, strings.TrimSpace(line),
				)

				break
			}
		}
	}
}

func setTitleFromFilename(meta *Meta, filename string) {
	base := filepath.Base(filename)
	title := strings.TrimSuffix(base, filepath.Ext(base))
	title = strings.ReplaceAll(title, "_", " ")
	title = strings.ReplaceAll(title, "-", " ")
	meta.Title = cases.Title(language.English).String(title)
}

// ExtractDocumentLeadingH1 will extract leading H1 heading
func ExtractDocumentLeadingH1(doc ast.Node, markdown []byte) string {
	var h1Text string
	// Walk the AST to find the first Level 1 Heading
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if heading, ok := n.(*ast.Heading); ok && heading.Level == 1 {
				var buf strings.Builder
				_ = ast.Walk(heading, func(child ast.Node, childEntering bool) (ast.WalkStatus, error) {
					if childEntering && child.Kind() == ast.KindText {
						buf.Write(child.(*ast.Text).Value(markdown))
					}
					return ast.WalkContinue, nil
				})
				h1Text = buf.String()
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})

	return h1Text
}

const (
	commentOpen  = "<!--"
	commentClose = "-->"
)

// parseHeaderComment checks if a line is a valid metadata header comment of the form "<!-- key: value -->".
func parseHeaderComment(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, commentOpen) || !strings.HasSuffix(line, commentClose) {
		return "", "", false
	}

	// The prefix and suffix can overlap: in "<!-->" both match, because the
	// "-->" is found starting at index 2, inside the "<!--". Slicing then asks
	// for line[4:2] and panics. Requiring room for both delimiters separately
	// is what makes the slice below safe -- this is the same class of crash as
	// issue #686.
	if len(line) < len(commentOpen)+len(commentClose) {
		return "", "", false
	}

	// Strip "<!--" and "-->"
	content := line[len(commentOpen) : len(line)-len(commentClose)]
	content = strings.TrimSpace(content)

	colonIdx := strings.Index(content, ":")
	if colonIdx == -1 {
		return "", "", false
	}

	key := strings.TrimSpace(content[:colonIdx])
	value := strings.TrimSpace(content[colonIdx+1:])
	return key, value, true
}

// Publish reports whether a document asks to be published.
//
// Saying nothing means yes: a repository full of documents that never mention
// synchronisation is the ordinary case, and it is the opting out that has to be
// deliberate.
func (meta *Meta) Publish() bool {
	if meta == nil || meta.Synchronized == nil {
		return true
	}

	return *meta.Synchronized
}
