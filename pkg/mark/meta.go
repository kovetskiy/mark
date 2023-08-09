package mark

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/reconquest/pkg/log"
)

const (
	HeaderParent      = `Parent`
	HeaderSpace       = `Space`
	HeaderType        = `Type`
	HeaderTitle       = `Title`
	HeaderLayout      = `Layout`
	HeaderAttachment  = `Attachment`
	HeaderLabel       = `Label`
	HeaderInclude     = `Include`
	HeaderSidebar     = `Sidebar`
	ContentAppearance = `Content-Appearance`
)

type Meta struct {
	Parents           []string
	Space             string
	Type              string
	Title             string
	Layout            string
	Sidebar           string
	Attachments       []string
	Labels            []string
	ContentAppearance string
}

const (
	FullWidthContentAppearance = "full-width"
	FixedContentAppearance     = "fixed"
)

var (
	reHeaderPatternV1    = regexp.MustCompile(`\[\]:\s*#\s*\(([^:]+):\s*(.*)\)`)
	reHeaderPatternV2    = regexp.MustCompile(`<!--\s*([^:]+):\s*(.*)\s*-->`)
	reHeaderPatternMacro = regexp.MustCompile(`<!-- Macro: .*`)
)

func ExtractMeta(data []byte, spaceFromCli string, titleFromH1 bool, parents []string) (*Meta, []byte, error) {
	var (
		meta   *Meta
		offset int
	)

	scanner := bufio.NewScanner(bytes.NewBuffer(data))
	for scanner.Scan() {
		line := scanner.Text()

		if err := scanner.Err(); err != nil {
			return nil, nil, err
		}

		offset += len(line) + 1

		matches := reHeaderPatternV2.FindStringSubmatch(line)
		if matches == nil {
			matches = reHeaderPatternV1.FindStringSubmatch(line)
			if matches == nil {
				matches = reHeaderPatternMacro.FindStringSubmatch(line)
				// If we have a match, then we started reading a macro.
				// We want to keep it in the document for it to be read by ExtractMacros
				if matches != nil {
					offset -= len(line) + 1
				}
				break
			}

			log.Warningf(
				fmt.Errorf(`legacy header usage found: %s`, line),
				"please use new header format: <!-- %s: %s -->",
				matches[1],
				matches[2],
			)
		}

		if meta == nil {
			meta = &Meta{}
			meta.Type = "page"                                  // Default if not specified
			meta.ContentAppearance = FullWidthContentAppearance // Default to full-width for backwards compatibility
		}

		//nolint:staticcheck
		header := strings.Title(matches[1])

		var value string
		if len(matches) > 1 {
			value = strings.TrimSpace(matches[2])
		}

		switch header {
		case HeaderParent:
			meta.Parents = append(meta.Parents, value)

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

		case HeaderAttachment:
			meta.Attachments = append(meta.Attachments, value)

		case HeaderLabel:
			meta.Labels = append(meta.Labels, value)

		case HeaderInclude:
			// Includes are parsed by a different func
			continue

		case ContentAppearance:
			if strings.TrimSpace(value) == FixedContentAppearance {
				meta.ContentAppearance = FixedContentAppearance
			} else {
				meta.ContentAppearance = FullWidthContentAppearance
			}

		default:
			log.Errorf(
				nil,
				`encountered unknown header %q line: %#v`,
				header,
				line,
			)

			continue
		}
	}

	if titleFromH1 || spaceFromCli != "" {
		if meta == nil {
			meta = &Meta{}
		}

		if meta.Type == "" {
			meta.Type = "page"
		}

		if meta.ContentAppearance == "" {
			meta.ContentAppearance = FullWidthContentAppearance // Default to full-width for backwards compatibility
		}

		if titleFromH1 && meta.Title == "" {
			meta.Title = ExtractDocumentLeadingH1(data)
		}
		if spaceFromCli != "" && meta.Space == "" {
			meta.Space = spaceFromCli
		}
	}

	if meta == nil {
		return nil, data, nil
	}

	// Prepend parent pages that are defined via the cli flag
	if len(parents) > 0 && parents[0] != "" {
		meta.Parents = append(parents, meta.Parents...)
	}

	return meta, data[offset:], nil
}
