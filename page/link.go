package page

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/confluence"
	"github.com/kovetskiy/mark/metadata"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

type LinkSubstitution struct {
	From string
	To   string
}

type markdownLink struct {
	full     string
	filename string
	hash     string
}

func ResolveRelativeLinks(
	api *confluence.API,
	meta *metadata.Meta,
	markdown []byte,
	base string,
	spaceFromCli string,
	titleFromH1 bool,
	titleFromFilename bool,
	parents []string,
	titleAppendGeneratedHash bool,
) ([]LinkSubstitution, error) {
	matches := parseLinks(string(markdown))

	// If the user didn't provide --space, inherit the current document's space so
	// relative links can be resolved within the same space.
	spaceForLinks := spaceFromCli
	if spaceForLinks == "" && meta != nil {
		spaceForLinks = meta.Space
	}

	links := []LinkSubstitution{}
	for _, match := range matches {
		log.Tracef(
			nil,
			"found a relative link: full=%s filename=%s hash=%s",
			match.full,
			match.filename,
			match.hash,
		)
		resolved, err := resolveLink(api, base, match, spaceForLinks, titleFromH1, titleFromFilename, parents, titleAppendGeneratedHash)
		if err != nil {
			return nil, karma.Format(err, "resolve link: %q", match.full)
		}

		if resolved == "" {
			continue
		}

		links = append(links, LinkSubstitution{
			From: match.full,
			To:   resolved,
		})
	}

	return links, nil
}

func resolveLink(
	api *confluence.API,
	base string,
	link markdownLink,
	spaceForLinks string,
	titleFromH1 bool,
	titleFromFilename bool,
	parents []string,
	titleAppendGeneratedHash bool,
) (string, error) {
	var result string

	if len(link.filename) > 0 {
		filepath := filepath.Join(base, link.filename)

		log.Tracef(nil, "filepath: %s", filepath)
		stat, err := os.Stat(filepath)
		if err != nil {
			return "", nil
		}

		if stat.IsDir() {
			return "", nil
		}

		linkContents, err := os.ReadFile(filepath)

		contentType := http.DetectContentType(linkContents)
		// Check if the MIME type starts with "text/"
		if !strings.HasPrefix(contentType, "text/") {
			log.Debugf(nil, "Ignoring link to file %q: detected content type %v", filepath, contentType)
			return "", nil
		}

		if err != nil {
			return "", karma.Format(err, "read file: %s", filepath)
		}

		linkContents = bytes.ReplaceAll(
			linkContents,
			[]byte("\r\n"),
			[]byte("\n"),
		)

		// This helps to determine if found link points to file that's
		// not markdown or have mark required metadata
		linkMeta, _, err := metadata.ExtractMeta(linkContents, spaceForLinks, titleFromH1, titleFromFilename, filepath, parents, titleAppendGeneratedHash)
		if err != nil {
			log.Errorf(
				err,
				"unable to extract metadata from %q; ignoring the relative link",
				filepath,
			)

			return "", nil
		}

		if linkMeta == nil {
			return "", nil
		}

		log.Tracef(
			nil,
			"extracted metadata: space=%s title=%s",
			linkMeta.Space,
			linkMeta.Title,
		)

		result, err = getConfluenceLink(api, linkMeta.Space, linkMeta.Title)
		if err != nil {
			return "", karma.Format(
				err,
				"find confluence page: %s / %s / %s",
				filepath,
				linkMeta.Space,
				linkMeta.Title,
			)
		}

		if result == "" {
			return "", nil
		}
	}

	if len(link.hash) > 0 {
		result = result + "#" + link.hash
	}

	return result, nil
}

func SubstituteLinks(markdown []byte, links []LinkSubstitution) []byte {
	for _, link := range links {
		if link.From == link.To {
			continue
		}

		log.Tracef(nil, "substitute link: %q -> %q", link.From, link.To)

		markdown = bytes.ReplaceAll(
			markdown,
			[]byte(fmt.Sprintf("](%s)", link.From)),
			[]byte(fmt.Sprintf("](%s)", link.To)),
		)
	}

	return markdown
}

func parseLinks(markdown string) []markdownLink {
	// Matches links but not inline images
	re := regexp.MustCompile(`[^\!]\[.+\]\((([^\)#]+)?#?([^\)]+)?)\)`)
	matches := re.FindAllStringSubmatch(markdown, -1)

	links := make([]markdownLink, len(matches))
	for i, match := range matches {
		links[i] = markdownLink{
			full:     match[1],
			filename: match[2],
			hash:     match[3],
		}
	}

	return links
}

// getConfluenceLink build (to be) link for Confluence, and tries to verify from
// API if there's real link available
func getConfluenceLink(
	api *confluence.API,
	space, title string,
) (string, error) {
	page, err := api.FindPage(space, title, "page")
	if err != nil {
		return "", karma.Format(err, "api: find page")
	}
	if page == nil {
		// Without a page ID there is no stable way to produce
		// /wiki/spaces/<space>/pages/<id>/<name>.
		return "", nil
	}

	// Confluence Cloud web UI URLs can be returned either as a path ("/wiki/..." or
	// "/ex/confluence/<cloudId>/wiki/...") or as a full absolute URL.
	absolute, err := makeAbsoluteConfluenceWebUIURL(api.BaseURL, page.Links.Full)
	if err != nil {
		return "", karma.Format(err, "build confluence webui URL")
	}

	return absolute, nil
}

func makeAbsoluteConfluenceWebUIURL(baseURL string, webui string) (string, error) {
	if webui == "" {
		return "", nil
	}

	u, err := url.Parse(webui)
	if err != nil {
		return "", err
	}

	path := normalizeConfluenceWebUIPath(u.Path)
	if path == "" {
		return "", nil
	}

	// If Confluence returns an absolute URL, trust its host/scheme.
	if u.Scheme != "" && u.Host != "" {
		baseURL = u.Scheme + "://" + u.Host
	}

	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	result := baseURL + path
	if u.RawQuery != "" {
		result += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		result += "#" + u.Fragment
	}

	return result, nil
}

// normalizeConfluenceWebUIPath rewrites Confluence Cloud "experience" URLs
// ("/ex/confluence/<cloudId>/wiki/..."), to canonical wiki paths ("/wiki/...").
func normalizeConfluenceWebUIPath(path string) string {
	if path == "" {
		return path
	}

	re := regexp.MustCompile(`^/ex/confluence/[^/]+(/wiki/.*)$`)
	match := re.FindStringSubmatch(path)
	if len(match) == 2 {
		return match[1]
	}

	return path
}
