package metadata

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
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
	HeaderParent      = `Parent`
	HeaderFolder      = `Folder`
	HeaderSpace       = `Space`
	HeaderType        = `Type`
	HeaderTitle       = `Title`
	HeaderLayout      = `Layout`
	HeaderEmoji       = `Emoji`
	HeaderAttachment  = `Attachment`
	HeaderLabel       = `Label`
	HeaderInclude     = `Include`
	HeaderSidebar     = `Sidebar`
	ContentAppearance = `Content-Appearance`
	HeaderImageAlign  = `Image-Align`
)

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
	ImageAlign        string
}

const (
	FullWidthContentAppearance = "full-width"
	FixedContentAppearance     = "fixed"
	DefaultContentAppearance   = "default"
)

type yamlFrontMatter struct {
	Parents                     []string `yaml:"parents"`
	Parent                      string   `yaml:"parent"`
	Folders                     []string `yaml:"folders"`
	Folder                      string   `yaml:"folder"`
	Space                       string   `yaml:"space"`
	Type                        string   `yaml:"type"`
	Title                       string   `yaml:"title"`
	Layout                      string   `yaml:"layout"`
	Sidebar                     string   `yaml:"sidebar"`
	Emoji                       string   `yaml:"emoji"`
	Attachments                 []string `yaml:"attachments"`
	Attachment                  string   `yaml:"attachment"`
	Labels                      []string `yaml:"labels"`
	Label                       string   `yaml:"label"`
	ContentAppearance           string   `yaml:"content-appearance"`
	ContentAppearanceUnderscore string   `yaml:"content_appearance"`
	ImageAlign                  string   `yaml:"image-align"`
	ImageAlignUnderscore        string   `yaml:"image_align"`
}

func (frontMatter yamlFrontMatter) meta() *Meta {
	meta := &Meta{
		Space:   strings.TrimSpace(frontMatter.Space),
		Type:    strings.TrimSpace(frontMatter.Type),
		Title:   strings.TrimSpace(frontMatter.Title),
		Layout:  strings.TrimSpace(frontMatter.Layout),
		Sidebar: strings.TrimSpace(frontMatter.Sidebar),
		Emoji:   strings.TrimSpace(frontMatter.Emoji),
	}
	if meta.Type == "" {
		meta.Type = "page"
	}
	if meta.Sidebar != "" {
		meta.Layout = "article"
	}

	meta.Parents = appendTrimmed(meta.Parents, frontMatter.Parent)
	meta.Parents = appendTrimmed(meta.Parents, frontMatter.Parents...)
	meta.Folders = appendTrimmed(meta.Folders, frontMatter.Folder)
	meta.Folders = appendTrimmed(meta.Folders, frontMatter.Folders...)
	meta.Attachments = appendTrimmed(meta.Attachments, frontMatter.Attachment)
	meta.Attachments = appendTrimmed(meta.Attachments, frontMatter.Attachments...)
	meta.Labels = appendTrimmed(meta.Labels, frontMatter.Label)
	meta.Labels = appendTrimmed(meta.Labels, frontMatter.Labels...)

	if value := firstNonEmpty(frontMatter.ContentAppearance, frontMatter.ContentAppearanceUnderscore); value != "" {
		setContentAppearance(meta, value)
	}
	meta.ImageAlign = strings.ToLower(strings.TrimSpace(firstNonEmpty(frontMatter.ImageAlign, frontMatter.ImageAlignUnderscore)))

	return meta
}

func appendTrimmed(dst []string, values ...string) []string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			dst = append(dst, value)
		}
	}
	return dst
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
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
	delimiter = bytes.TrimSuffix(delimiter, []byte("\r"))

	for {
		line, remaining, hasNewline := bytes.Cut(rest, []byte("\n"))
		if bytes.Equal(bytes.TrimSuffix(line, []byte("\r")), delimiter) {
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
		var parsed yamlFrontMatter
		if err := frontMatterData.Decode(&parsed); err != nil {
			return nil, nil, fmt.Errorf("decode YAML front matter: %w", err)
		}

		meta = parsed.meta()
		var err error
		body, err = stripFrontMatter(data)
		if err != nil {
			return nil, nil, err
		}
	}

	var lastStop int
	shouldBreak := false

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		if htmlBlock, ok := child.(*ast.HTMLBlock); ok {
			lines := htmlBlock.Lines()
			if lines.Len() > 0 {
				if lastStop > 0 && lines.At(0).Start != lastStop {
					break
				}
			}

			for i := 0; i < lines.Len(); i++ {
				lineSeg := lines.At(i)
				line := string(lineSeg.Value(data))

				key, value, ok := parseHeaderComment(line)
				if !ok {
					shouldBreak = true
					break
				}

				if meta == nil {
					meta = &Meta{}
					meta.Type = "page" // Default if not specified
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

				case HeaderAttachment:
					meta.Attachments = append(meta.Attachments, value)

				case HeaderLabel:
					meta.Labels = append(meta.Labels, value)

				case HeaderInclude:
					// Includes are parsed by a different func
					lastStop = lineSeg.Stop
					continue

				case ContentAppearance:
					setContentAppearance(meta, value)

				case HeaderImageAlign:
					meta.ImageAlign = strings.ToLower(strings.TrimSpace(value))

				default:
					log.Error().
						Err(nil).
						Msgf(`encountered unknown header %q line: %#v`, header, line)
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
		body = data[lastStop:]
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

// parseHeaderComment checks if a line is a valid metadata header comment of the form "<!-- key: value -->".
func parseHeaderComment(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "<!--") || !strings.HasSuffix(line, "-->") {
		return "", "", false
	}

	// Strip "<!--" and "-->"
	content := line[4 : len(line)-3]
	content = strings.TrimSpace(content)

	colonIdx := strings.Index(content, ":")
	if colonIdx == -1 {
		return "", "", false
	}

	key := strings.TrimSpace(content[:colonIdx])
	value := strings.TrimSpace(content[colonIdx+1:])
	return key, value, true
}
