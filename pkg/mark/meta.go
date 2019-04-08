package mark

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

const (
	HeaderParent string = `Parent`
	HeaderSpace         = `Space`
	HeaderTitle         = `Title`
	HeaderLayout        = `Layout`
)

type Meta struct {
	Parents []string
	Space   string
	Title   string
	Layout  string
}

func ExtractMeta(data []byte) (*Meta, error) {
	headerPattern := regexp.MustCompile(`\[\]:\s*#\s*\(([^:]+):\s*(.*)\)`)

	var meta *Meta

	scanner := bufio.NewScanner(bytes.NewBuffer(data))
	for scanner.Scan() {
		line := scanner.Text()

		if err := scanner.Err(); err != nil {
			return nil, err
		}

		matches := headerPattern.FindStringSubmatch(line)
		if matches == nil {
			break
		}

		if meta == nil {
			meta = &Meta{}
		}

		header := strings.Title(matches[1])

		switch header {
		case HeaderParent:
			meta.Parents = append(meta.Parents, matches[2])

		case HeaderSpace:
			meta.Space = strings.ToUpper(matches[2])

		case HeaderTitle:
			meta.Title = strings.TrimSpace(matches[2])

		case HeaderLayout:
			meta.Layout = strings.TrimSpace(matches[2])

		default:
			logger.Errorf(
				`encountered unknown header '%s' line: %#v`,
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
