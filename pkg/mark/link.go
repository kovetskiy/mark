package mark

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/kovetskiy/mark/pkg/confluence"
)

type Link struct {
	MDLink string
	Link   string
}

// ResolveRelativeLinks finds links in the markdown, and replaces one pointing
// to other Markdowns either by own link (if not created to Confluence yet) or
// witn actual Confluence link
func ResolveRelativeLinks(
	api *confluence.API,
	markdown []byte,
	base string,
) (links []Link, collectedErrors error) {
	currentMarkdownMetadata, onlyMarkdown, err := ExtractMeta(markdown)
	if err != nil {
		return links, fmt.Errorf("unable to get metadata from handled markdown file. Error %w", err)
	}

	currentPageLinkString, collectedErrors := getConfluenceLink(api, currentMarkdownMetadata.Space, currentMarkdownMetadata.Title, collectedErrors)

	submatchall := collectLinksFromMarkdown(string(onlyMarkdown))

	for _, element := range submatchall {
		link := Link{
			MDLink: element[1],
			Link:   currentPageLinkString,
		}
		// If link points to markdown like target, we build link for that in Confluence
		if len(element[2]) > 0 {
			possibleMDFile := element[2]
			filepath := filepath.Join(base, possibleMDFile)
			if _, err := os.Stat(filepath); err == nil {
				linkMarkdown, err := ioutil.ReadFile(filepath)
				if err != nil {
					collectedErrors = fmt.Errorf("%w\n unable to read markdown file "+filepath, collectedErrors)
					continue
				}
				// This helps to determine if found link points to file that's not markdown
				// or have mark required metadata
				meta, _, err := ExtractMeta(linkMarkdown)
				if err != nil {
					collectedErrors = fmt.Errorf("%w\n unable to get metadata from markdown file "+filepath, collectedErrors)
					continue
				}

				link.Link, collectedErrors = getConfluenceLink(api, meta.Space, meta.Title, collectedErrors)
			}
		}

		if len(element[3]) > 0 {
			link.Link = currentPageLinkString + "#" + element[2]
		}

		links = append(links, link)
	}
	return links, collectedErrors
}

// ReplaceRelativeLinks replaces relative links between md files (in same
// directory structure) with links working in Confluence
func ReplaceRelativeLinks(markdown []byte, links []Link) []byte {
	for _, link := range links {
		markdown = bytes.ReplaceAll(
			markdown,
			[]byte(fmt.Sprintf("](%s)", link.MDLink)),
			[]byte(fmt.Sprintf("](%s)", link.Link)),
		)
	}
	return markdown
}

// collectLinksFromMarkdown collects all links from given markdown file
// (including images and external links)
func collectLinksFromMarkdown(markdown string) [][]string {
	re := regexp.MustCompile("\\[[^\\]]+\\]\\((([^\\)#]+)?#?([^\\)]+)?)\\)")
	return re.FindAllStringSubmatch(markdown, -1)
}

// getConfluenceLink build (to be) link for Conflunce, and tries to verify from API if there's real link available
func getConfluenceLink(api *confluence.API, space, title string, collectedErrors error) (string, error) {
	link := fmt.Sprintf("%s/display/%s/%s", api.BaseURL, space, url.QueryEscape(title))
	confluencePage, err := api.FindPage(space, title)
	if err != nil {
		collectedErrors = fmt.Errorf("%w\n "+err.Error(), collectedErrors)
	} else if confluencePage != nil {
		// Needs baseURL, as REST api response URL doesn't contain subpath ir confluence is server from that
		link = api.BaseURL + confluencePage.Links.Full
	}

	return link, collectedErrors
}
