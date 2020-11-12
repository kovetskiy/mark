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
	"github.com/reconquest/pkg/log"
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
) ([]Link, error) {
	links := []Link{}
	re := regexp.MustCompile("\\[.*?\\]\\((.*?)\\)")
	submatchall := re.FindAllStringSubmatch(string(markdown), -1)
	for _, element := range submatchall {
		possibleMDFile := element[1]
		filepath := filepath.Join(base, possibleMDFile)
		if _, err := os.Stat(filepath); err == nil {
			linkMarkdown, err := ioutil.ReadFile(filepath)
			if err != nil {
				log.Errorf(err, "unable to read markdown file %s", filepath)
				continue
			}
			meta, _, err := ExtractMeta(linkMarkdown)
			if err != nil {
				log.Errorf(err, "unable to get metadata from markdown file %s", filepath)
				continue
			}
			link := fmt.Sprintf("%s/display/%s/%s", api.BaseURL, meta.Space, url.QueryEscape(meta.Title))
			confluencePage, err := api.FindPage(meta.Space, meta.Title)
			if err == nil && confluencePage != nil {
				// Needs baseURL, as REST api response URL doesn't contain subpath ir confluence is server from that
				link = api.BaseURL + confluencePage.Links.Full
			}

			links = append(links, Link{
				MDLink: possibleMDFile,
				Link:   link,
			})
		}
	}
	return links, nil
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
