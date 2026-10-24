package export

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/page"
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
	tinyPath = regexp.MustCompile(`^/x/([^/]+)/?$`)

	// viewPath is the address of a page as a reader opens it: the forms above,
	// less a dated /display/ path, which names a blog post by its title, and
	// with /pages/viewpage.action the only one of the *.action forms that carry
	// a pageId. editpage.action, viewinfo.action and the like lead somewhere
	// else than the page itself.
	viewPath = regexp.MustCompile(`^/(?:` +
		`spaces/[^/]+/pages/[0-9]+(?:/[^/]*)?|` +
		`spaces/[^/]+/blog/(?:[0-9]{4}/[0-9]{2}/[0-9]{2}/)?[0-9]+(?:/[^/]*)?|` +
		`pages/viewpage\.action|` +
		`display/[^/]+/[^/]+|` +
		`x/[^/]+` +
		`)$`)
)

// ParsePageURL reads the page a Confluence URL points at: the address a
// browser shows for a page on Cloud or on Server and Data Center. The base URL
// is read as --target-url reads it. A short link (/x/...) names the page by
// its id, encoded, and is read without asking Confluence.
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

	if m := tinyPath.FindStringSubmatch(path); m != nil {
		id, err := page.DecodeTinyLinkID(m[1])
		if err != nil {
			return PageRef{}, fmt.Errorf("unable to read the short link %q: %w", raw, err)
		}
		ref.PageID = id
		return ref, nil
	}

	return PageRef{}, fmt.Errorf("%q is not the URL of a Confluence page mark recognises", raw)
}

// pageOnInstance reads the page a URL pasted into a page points at, when it
// is a page of the instance at base and the URL opens it to be read. A URL
// carrying a fragment is left alone: it names a place on the page by
// Confluence's anchor, which mark's own heading anchors need not match.
func pageOnInstance(href, base string) (PageRef, bool) {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Fragment != "" {
		return PageRef{}, false
	}

	ref, err := ParsePageURL(href)
	if err != nil || !strings.EqualFold(strings.TrimSuffix(ref.BaseURL, "/"), strings.TrimSuffix(base, "/")) {
		return PageRef{}, false
	}

	contextPath := strings.TrimPrefix(ref.BaseURL, parsed.Scheme+"://"+parsed.Host)
	path := strings.TrimPrefix(strings.TrimSuffix(parsed.EscapedPath(), "/"), contextPath)
	if !viewPath.MatchString(path) {
		return PageRef{}, false
	}

	return ref, true
}

// lookup is a map lookup a function can take as its arguments.
func lookup(m map[string]string, key string) (string, bool) {
	value, ok := m[key]
	return value, ok
}
