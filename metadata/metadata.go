package metadata

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
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

var (
	reHeaderPatternV2 = regexp.MustCompile(`<!--\s*([^:]+):\s*(.*)\s*-->`)
)

func ExtractMeta(data []byte, spaceFromCli string, titleFromH1 bool, titleFromFilename bool, filename string, parents []string, titleAppendGeneratedHash bool, defaultContentAppearance string) (*Meta, []byte, error) {
	var (
		meta   *Meta
		offset int
	)

	reader := text.NewReader(data)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(reader)

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

				matches := reHeaderPatternV2.FindStringSubmatch(line)
				if matches == nil {
					shouldBreak = true
					break
				}

				if meta == nil {
					meta = &Meta{}
					meta.Type = "page" // Default if not specified
				}

				header := cases.Title(language.English).String(matches[1])

				var value string
				if len(matches) > 2 {
					value = strings.TrimSpace(matches[2])
				}

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
					switch strings.TrimSpace(value) {
					case FixedContentAppearance:
						meta.ContentAppearance = FixedContentAppearance
					case DefaultContentAppearance:
						meta.ContentAppearance = DefaultContentAppearance
					default:
						meta.ContentAppearance = FullWidthContentAppearance
					}

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

	offset = lastStop

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
		switch strings.TrimSpace(defaultContentAppearance) {
		case FixedContentAppearance:
			meta.ContentAppearance = FixedContentAppearance
		case DefaultContentAppearance:
			meta.ContentAppearance = DefaultContentAppearance
		default:
			meta.ContentAppearance = FullWidthContentAppearance
		}
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
	return meta, data[offset:], nil
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
