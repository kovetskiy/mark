package export

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
)

var (
	// viewPath is the address of a page as a reader opens it: Cloud, /display/ and /x/ forms,
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

// pageOnInstance reads the page a URL pasted into a page points at, when it
// is a page of the instance at base and the URL opens it to be read. A URL
// carrying a fragment is left alone: it names a place on the page by
// Confluence's anchor, which mark's own heading anchors need not match. So is
// one with a query, other than viewpage.action's pageId: a focused comment or
// an old version of the page would be lost.
func pageOnInstance(href, base string) (confluence.PageRef, bool) {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Fragment != "" || !onlyPageID(parsed) {
		return confluence.PageRef{}, false
	}

	ref, err := confluence.ParsePageURL(href)
	// Both ways: configured on the gateway, SameInstance takes any Cloud
	// site, but a link names a page of one site only.
	if err != nil || confluence.SameInstance(base, ref.BaseURL) != nil || confluence.SameInstance(ref.BaseURL, base) != nil {
		return confluence.PageRef{}, false
	}

	// SameInstance lets a page URL leave the context path out, which is the
	// same server but not a page link that opens: an instance under /wiki
	// does not serve /display/... at the root.
	contextPath := strings.TrimPrefix(ref.BaseURL, parsed.Scheme+"://"+parsed.Host)
	if instance, err := url.Parse(base); err != nil || contextPath == "" && strings.Trim(instance.EscapedPath(), "/") != "" {
		return confluence.PageRef{}, false
	}
	path := strings.TrimPrefix(strings.TrimSuffix(parsed.EscapedPath(), "/"), contextPath)
	if !viewPath.MatchString(path) {
		return confluence.PageRef{}, false
	}

	return ref, true
}

// onlyPageID reports whether a URL has no query, or is viewpage.action naming
// the page and nothing else. A pageId on any other form would name another
// page than its path does, and win.
func onlyPageID(u *url.URL) bool {
	query := u.Query()
	if len(query) == 0 {
		return true
	}
	return strings.HasSuffix(u.Path, "/pages/viewpage.action") && len(query) == 1 && len(query["pageId"]) == 1
}
