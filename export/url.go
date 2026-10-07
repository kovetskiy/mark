package export

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
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
	// /wiki/spaces/KEY/pages/123/Title, and the editor's /pages/edit-v2/123.
	cloudPagePath = regexp.MustCompile(`^(.*?)/spaces/([^/]+)/pages/(?:edit(?:-v2)?/)?([0-9]+)(?:/.*)?$`)
	// /wiki/spaces/KEY/blog/2024/01/31/123/Title
	cloudBlogPath = regexp.MustCompile(`^(.*?)/spaces/([^/]+)/blog/(?:[0-9]{4}/[0-9]{2}/[0-9]{2}/)?([0-9]+)(?:/.*)?$`)
	// /display/KEY/Title and /display/KEY/2024/01/31/Title
	displayPath = regexp.MustCompile(`^(.*?)/display/([^/]+)/(?:[0-9]{4}/[0-9]{2}/[0-9]{2}/)?([^/]+)/?$`)
	// /pages/viewpage.action?pageId=123
	actionPath = regexp.MustCompile(`^(.*?)/pages/[^/]+\.action$`)
	// /x/AbCd
	tinyPath = regexp.MustCompile(`^(.*?)/x/[^/]+/?$`)
)

// ParsePageURL reads the page a Confluence URL points at: the address a
// browser shows for a page on Cloud or on Server and Data Center.
func ParsePageURL(raw string) (PageRef, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return PageRef{}, fmt.Errorf("unable to parse %q as a URL: %w", raw, err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return PageRef{}, fmt.Errorf("%q is not the URL of a Confluence page: it names no host", raw)
	}

	site := parsed.Scheme + "://" + parsed.Host
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")

	if id := parsed.Query().Get("pageId"); id != "" {
		prefix := ""
		if m := actionPath.FindStringSubmatch(path); m != nil {
			prefix = m[1]
		}
		return PageRef{BaseURL: site + prefix, PageID: id}, nil
	}

	for _, pattern := range []*regexp.Regexp{cloudPagePath, cloudBlogPath} {
		if m := pattern.FindStringSubmatch(path); m != nil {
			space, err := url.PathUnescape(m[2])
			if err != nil {
				return PageRef{}, fmt.Errorf("unable to read the space of %q: %w", raw, err)
			}
			return PageRef{BaseURL: site + m[1], PageID: m[3], Space: space}, nil
		}
	}

	if m := displayPath.FindStringSubmatch(path); m != nil {
		space, err := url.PathUnescape(m[2])
		if err != nil {
			return PageRef{}, fmt.Errorf("unable to read the space of %q: %w", raw, err)
		}
		// The title is written with "+" for a space, the way a query is.
		title, err := url.QueryUnescape(m[3])
		if err != nil {
			return PageRef{}, fmt.Errorf("unable to read the title of %q: %w", raw, err)
		}
		return PageRef{BaseURL: site + m[1], Space: space, Title: title}, nil
	}

	if tinyPath.MatchString(path) {
		return PageRef{}, errors.New(
			"a short link (/x/...) does not say which page it is without asking Confluence; " +
				"open it and use the address it leads to, or --page-id",
		)
	}

	return PageRef{}, fmt.Errorf("%q is not the URL of a Confluence page mark recognises", raw)
}
