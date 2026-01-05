package page

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// getConfluenceLink builds a stable Confluence tiny link for the given page.
// Tiny links use the format {baseURL}/x/{encodedPageID} and are immune to
// Cloud-specific URL variations like /ex/confluence/<cloudId>/wiki/...
func getConfluenceLink(
	api *confluence.API,
	space, title string,
) (string, error) {
	page, err := api.FindPage(space, title, "page")
	if err != nil {
		return "", karma.Format(err, "api: find page")
	}
	if page == nil {
		return "", nil
	}

	tiny, err := GenerateTinyLink(api.BaseURL, page.ID)
	if err != nil {
		return "", karma.Format(err, "generate tiny link for page %s", page.ID)
	}

	return tiny, nil
}

// GenerateTinyLink generates a Confluence tiny link from a page ID.
// The algorithm converts the page ID to a little-endian 32-bit byte array,
// base64-encodes it, and applies URL-safe transformations.
// Format: {baseURL}/x/{encodedID}
//
// Reference: https://support.atlassian.com/confluence/kb/how-to-programmatically-generate-the-tiny-link-of-a-confluence-page
func GenerateTinyLink(baseURL string, pageID string) (string, error) {
	id, err := strconv.ParseUint(pageID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid page ID %q: %w", pageID, err)
	}

	encoded := encodeTinyLinkID(id)
	baseURL = strings.TrimSuffix(baseURL, "/")

	return baseURL + "/x/" + encoded, nil
}

// encodeTinyLinkID encodes a page ID into the Confluence tiny link format.
// This is the core algorithm extracted for testability.
func encodeTinyLinkID(id uint64) string {
	// Pack as little-endian. Use 8 bytes to support large page IDs,
	// but the base64 trimming will remove unnecessary trailing zeros.
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, id)

	// Trim trailing zero bytes (they become 'A' padding in base64)
	for len(buf) > 1 && buf[len(buf)-1] == 0 {
		buf = buf[:len(buf)-1]
	}

	// Base64 encode
	encoded := base64.StdEncoding.EncodeToString(buf)

	// Transform to URL-safe format:
	// - Strip '=' padding
	// - Replace '/' with '-'
	// - Replace '+' with '_'
	var result strings.Builder
	for _, c := range encoded {
		switch c {
		case '=':
			continue
		case '/':
			result.WriteByte('-')
		case '+':
			result.WriteByte('_')
		default:
			result.WriteRune(c)
		}
	}

	return result.String()
}
