package confluence

import (
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

// ContentRef names content by ID, a page title, or an attachment's container and filename.
type ContentRef struct {
	BaseURL     string
	ID          string
	Space       string
	Title       string
	ContainerID string
}

var (
	// /spaces/KEY/pages/123/Title, and the editor's /pages/edit-v2/123.
	cloudPagePath = regexp.MustCompile(`^/spaces/([^/]+)/pages/`)
	// /spaces/KEY/blog/2024/01/31/123/Title
	cloudBlogPath = regexp.MustCompile(`^/spaces/([^/]+)/blog/(?:[0-9]{4}/[0-9]{2}/[0-9]{2}/)?([0-9]+)(?:/.*)?$`)
	// /display/KEY/Title and /display/KEY/2024/01/31/Title
	displayPath = regexp.MustCompile(`^/display/([^/]+)/(?:[0-9]{4}/[0-9]{2}/[0-9]{2}/)?([^/]+)/?$`)
	// /x/AbCd
	tinyPath               = regexp.MustCompile(`^/x/([^/]+)/?$`)
	attachmentDownloadPath = regexp.MustCompile(`^(.*)/download/attachments/([0-9]+)/([^/]+)/?$`)
	customContentPath      = regexp.MustCompile(`^(.*)/display/[^/]+/customcontent/([0-9]+)/?$`)
)

// ParseContentURL reads a URL for a page, blog post, attachment, or custom-content collection.
// Attachment downloads name a container and filename, which require an API lookup.
func ParseContentURL(raw, collection string) (ContentRef, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ContentRef{}, fmt.Errorf("unable to parse content URL: %w", err)
	}
	if _, err := originOf(parsed); err != nil {
		return ContentRef{}, fmt.Errorf("invalid content URL: %w", err)
	}
	switch collection {
	case LabelCollectionPages, LabelCollectionBlogposts:
		page, err := ParsePageURL(raw)
		return ContentRef{BaseURL: page.BaseURL, ID: page.PageID, Space: page.Space, Title: page.Title}, err
	case LabelCollectionCustomContent:
		if match := customContentPath.FindStringSubmatch(parsed.EscapedPath()); match != nil {
			return ContentRef{BaseURL: parsed.Scheme + "://" + parsed.Host + match[1], ID: match[2]}, nil
		}
	case LabelCollectionAttachments:
		if match := attachmentDownloadPath.FindStringSubmatch(parsed.EscapedPath()); match != nil {
			filename, err := url.PathUnescape(match[3])
			return ContentRef{BaseURL: parsed.Scheme + "://" + parsed.Host + match[1], ContainerID: match[2], Title: filename}, err
		}
		page, err := ParsePageURL(raw)
		if err != nil {
			return ContentRef{}, err
		}
		preview := strings.SplitN(strings.TrimPrefix(parsed.Query().Get("preview"), "/"), "/", 3)
		if len(preview) == 3 && preview[2] != "" {
			container, id := preview[0], preview[1]
			validIDs := container != "" && id != "" && strings.Trim(container+id, "0123456789") == ""
			if validIDs && (page.PageID == "" || page.PageID == container) {
				return ContentRef{BaseURL: page.BaseURL, ID: id}, nil
			}
		}
	}
	return ContentRef{}, fmt.Errorf("URL does not identify an item in %s", collection)
}

// ParsePageURL reads the page a Confluence URL points at: the address a
// browser shows for a page on Cloud or on Server and Data Center. The base URL
// is read as mark publish --url reads it. A short link (/x/...) names the page by
// its id, encoded, and is read without asking Confluence.
func ParsePageURL(raw string) (PageRef, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return PageRef{}, fmt.Errorf("unable to parse %q as a URL: %w", raw, err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return PageRef{}, fmt.Errorf("%q is not the URL of a Confluence page: it names no host", raw)
	}

	contextPath, pageID := SplitPageURL(parsed)
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
		id, err := DecodeTinyLinkID(m[1])
		if err != nil {
			return PageRef{}, fmt.Errorf("unable to read the short link %q: %w", raw, err)
		}
		ref.PageID = id
		return ref, nil
	}

	return PageRef{}, fmt.Errorf("%q is not the URL of a Confluence page mark recognises", raw)
}
