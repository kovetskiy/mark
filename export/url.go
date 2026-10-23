package export

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
)

// PageRef is a page as a URL names it: by id, or by space and title.
type PageRef struct {
	// BaseURL is the address of the Confluence instance the URL is on,
	// including the path it is served under ("/wiki" on Cloud).
	BaseURL string

	PageID string
	Space  string
	Title  string
}

var (
	// /spaces/KEY/pages/123/Title, and the editor's /pages/edit-v2/123.
	cloudPagePath = regexp.MustCompile(`^/spaces/([^/]+)/pages/`)
	// /spaces/KEY/blog/2024/01/31/123/Title
	cloudBlogPath = regexp.MustCompile(`^/spaces/([^/]+)/blog/(?:[0-9]{4}/[0-9]{2}/[0-9]{2}/)?([0-9]+)(?:/.*)?$`)
	// /display/KEY/Title and /display/KEY/2024/01/31/Title
	displayPath = regexp.MustCompile(`^/display/([^/]+)/(?:[0-9]{4}/[0-9]{2}/[0-9]{2}/)?([^/]+)/?$`)
	// /x/AbCd
	tinyPath = regexp.MustCompile(`^/x/[^/]+/?$`)
)

// ParsePageURL reads the page a Confluence URL points at: the address a
// browser shows for a page on Cloud or on Server and Data Center. The base URL
// is read as --target-url reads it.
func ParsePageURL(raw string) (PageRef, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return PageRef{}, fmt.Errorf("unable to parse %q as a URL: %w", raw, err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return PageRef{}, fmt.Errorf("%q is not the URL of a Confluence page: it names no host", raw)
	}

	contextPath, pageID := confluence.SplitPageURL(parsed)
	ref := PageRef{BaseURL: parsed.Scheme + "://" + parsed.Host + contextPath, PageID: pageID}
	path := strings.TrimPrefix(strings.TrimSuffix(parsed.EscapedPath(), "/"), contextPath)

	unescape := func(what, value string, unescape func(string) (string, error)) (string, error) {
		out, err := unescape(value)
		if err != nil {
			return "", fmt.Errorf("unable to read the %s of %q: %w", what, raw, err)
		}
		return out, nil
	}

	switch m := cloudPagePath.FindStringSubmatch(path); {
	case m != nil && pageID != "":
		ref.Space, err = unescape("space", m[1], url.PathUnescape)
		return ref, err
	case pageID != "":
		return ref, nil
	}

	if m := cloudBlogPath.FindStringSubmatch(path); m != nil {
		ref.PageID = m[2]
		ref.Space, err = unescape("space", m[1], url.PathUnescape)
		return ref, err
	}

	if m := displayPath.FindStringSubmatch(path); m != nil {
		if ref.Space, err = unescape("space", m[1], url.PathUnescape); err != nil {
			return PageRef{}, err
		}
		// The title is written with "+" for a space, the way a query is.
		ref.Title, err = unescape("title", m[2], url.QueryUnescape)
		return ref, err
	}

	if tinyPath.MatchString(path) {
		return PageRef{}, errors.New(
			"a short link (/x/...) does not say which page it is without asking Confluence; " +
				"open it and use the address it leads to, or --page-id",
		)
	}

	return PageRef{}, fmt.Errorf("%q is not the URL of a Confluence page mark recognises", raw)
}
