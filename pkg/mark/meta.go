package mark

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/pkg/log"
)

const (
	HeaderParent     = `Parent`
	HeaderSpace      = `Space`
	HeaderTitle      = `Title`
	HeaderLayout     = `Layout`
	HeaderAttachment = `Attachment`
)

type Meta struct {
	Parents     []string
	Space       string
	Title       string
	Layout      string
	Attachments []string
}

func ExtractMeta(data []byte) (*Meta, error) {
	var (
		headerPatternV1 = regexp.MustCompile(`\[\]:\s*#\s*\(([^:]+):\s*(.*)\)`)
		headerPatternV2 = regexp.MustCompile(`<!--\s*([^:]+):\s*(.*)\s*-->`)
	)

	var meta *Meta

	scanner := bufio.NewScanner(bytes.NewBuffer(data))
	for scanner.Scan() {
		line := scanner.Text()

		if err := scanner.Err(); err != nil {
			return nil, err
		}

		matches := headerPatternV2.FindStringSubmatch(line)
		if matches == nil {
			matches = headerPatternV1.FindStringSubmatch(line)
			if matches == nil {
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
		}

		header := strings.Title(matches[1])

		var value string
		if len(matches) > 1 {
			value = strings.TrimSpace(matches[2])
		}

		switch header {
		case HeaderParent:
			meta.Parents = append(meta.Parents, value)

		case HeaderSpace:
			meta.Space = strings.ToUpper(value)

		case HeaderTitle:
			meta.Title = strings.TrimSpace(value)

		case HeaderLayout:
			meta.Layout = strings.TrimSpace(value)

		case HeaderAttachment:
			meta.Attachments = append(meta.Attachments, value)

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

	if meta == nil {
		return nil, nil
	}

	if meta.Space == "" {
		return nil, fmt.Errorf(
			"space key is not set (%s header is not set)",
			HeaderSpace,
		)
	}

	if meta.Title == "" {
		return nil, fmt.Errorf(
			"page title is not set (%s header is not set)",
			HeaderTitle,
		)
	}

	return meta, nil
}
