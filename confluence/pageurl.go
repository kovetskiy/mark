package confluence

import (
	"net/url"
	"strings"
)

// pagePathSegments are the path segments at which a Confluence page URL stops
// naming the instance and starts naming something in it: the viewpage.action
// form, the /spaces/KEY/pages/ID/Title form Cloud uses, /display/KEY/Title, a
// /x/ tiny link, a plugin's page such as viewsource's, and the home page.
// Any *.action segment marks the same boundary.
var pagePathSegments = map[string]bool{
	"pages":   true,
	"spaces":  true,
	"display": true,
	"x":       true,
	"plugins": true,
	"home":    true,
}

// namesSomethingInTheInstance reports whether a path segment is where a URL
// stops naming the instance.
func namesSomethingInTheInstance(segment string) bool {
	return pagePathSegments[segment] || strings.HasSuffix(segment, ".action")
}

// SplitPageURL reads the context path and the page id out of the URL of a
// page, as --target-url and mark export --url take it.
//
// The context path is everything ahead of the part that names the page: /wiki
// on Cloud, whatever the instance is deployed under on Server -- /confluence is
// the usual one -- and nothing at all on an instance at the root. Taking only
// the scheme and host sent every REST call to https://x.atlassian.net/rest/api
// rather than https://x.atlassian.net/wiki/rest/api.
//
// A URL with none of those parts in it is taken to name the instance only when
// it names nothing else: no pageId, and a path of at most one plain segment,
// as in https://x.atlassian.net/wiki. Anything else is a page of a kind not
// recognised here, and its path says nothing reliable about the context path,
// so none is assumed -- the scheme and host, as before this was read at all.
//
// The page id comes from the pageId parameter of the viewpage.action form, or
// from the segment after pages/ in the /spaces/KEY/pages/ID/Title form, which is
// the one a browser shows on Cloud.
func SplitPageURL(target *url.URL) (contextPath, pageID string) {
	pageID = target.Query().Get("pageId")

	path := strings.TrimRight(target.EscapedPath(), "/")
	segments := strings.Split(path, "/")

	at := -1
	for i, segment := range segments {
		if i > 0 && namesSomethingInTheInstance(segment) {
			at = i

			break
		}
	}

	if at == -1 {
		if pageID == "" && len(segments) <= 2 && !strings.Contains(path, ".") {
			return path, pageID
		}

		return "", pageID
	}

	if pageID == "" && segments[at] == "spaces" {
		pageID = spacesPageID(segments[at:])
	}

	return strings.Join(segments[:at], "/"), pageID
}

// spacesPageID returns the page id out of spaces/KEY/pages/ID/Title, or "" when
// segments are not that. An edit URL puts a word ahead of the id --
// pages/edit-v2/ID -- so the id is the first number after pages/.
func spacesPageID(segments []string) string {
	if len(segments) < 4 || segments[2] != "pages" {
		return ""
	}

	for _, segment := range segments[3:] {
		if segment != "" && strings.Trim(segment, "0123456789") == "" {
			return segment
		}
	}

	return ""
}
