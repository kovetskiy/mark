package page

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/rs/zerolog/log"
)

type markdownLink struct {
	full     string
	filename string
	hash     string
}

// LinkResolver resolves one link target at a time, for a caller walking a
// parsed document rather than scanning its text.
type LinkResolver struct {
	API                      *confluence.API
	Base                     string
	SpaceForLinks            string
	TitleFromH1              bool
	TitleFromFilename        bool
	Parents                  []string
	TitleAppendGeneratedHash bool
	FrontMatterEnabled       bool
}

// NewLinkResolver builds a resolver for the document at base.
//
// A link with no space of its own is resolved in the document's own space,
// which is what a relative link between two files in one repository means.
func NewLinkResolver(
	api *confluence.API,
	meta *metadata.Meta,
	base string,
	spaceFromCli string,
	titleFromH1 bool,
	titleFromFilename bool,
	parents []string,
	titleAppendGeneratedHash bool,
	frontMatterEnabled bool,
) *LinkResolver {
	spaceForLinks := spaceFromCli
	if spaceForLinks == "" && meta != nil {
		spaceForLinks = meta.Space
	}

	return &LinkResolver{
		API:                      api,
		Base:                     base,
		SpaceForLinks:            spaceForLinks,
		TitleFromH1:              titleFromH1,
		TitleFromFilename:        titleFromFilename,
		Parents:                  parents,
		TitleAppendGeneratedHash: titleAppendGeneratedHash,
		FrontMatterEnabled:       frontMatterEnabled,
	}
}

// Resolve reports what a link target should become, or "" to leave it alone.
//
// The target arrives as the document wrote it -- a path with an optional
// #fragment -- which is the same shape the old scanner extracted by hand.
func (r *LinkResolver) Resolve(target string) (string, error) {
	if r == nil || r.API == nil || target == "" {
		return "", nil
	}

	// Anything with a scheme, or a bare fragment, is not a link to a file in
	// this repository and is left as written.
	if strings.Contains(target, "://") || strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, "mailto:") {
		return "", nil
	}

	filename, hash, _ := strings.Cut(target, "#")

	resolved, err := resolveLink(
		r.API, r.Base,
		markdownLink{full: target, filename: filename, hash: hash},
		r.SpaceForLinks, r.TitleFromH1, r.TitleFromFilename,
		r.Parents, r.TitleAppendGeneratedHash, r.FrontMatterEnabled,
	)
	if err != nil {
		return "", fmt.Errorf("resolve link %q: %w", target, err)
	}

	return resolved, nil
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
	frontMatterEnabled bool,
) (string, error) {
	var result string

	if len(link.filename) > 0 {
		filepath := filepath.Join(base, link.filename)

		log.Trace().Msgf("filepath: %s", filepath)
		stat, err := os.Stat(filepath)
		if err != nil {
			// Not a link to a file on disk (or unreadable): leave the link
			// untouched rather than failing the run. Swallowing err is
			// deliberate here.
			return "", nil //nolint:nilerr
		}

		if stat.IsDir() {
			return "", nil
		}

		linkContents, err := os.ReadFile(filepath)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", filepath, err)
		}

		contentType := http.DetectContentType(linkContents)
		// Check if the MIME type starts with "text/"
		if !strings.HasPrefix(contentType, "text/") {
			log.Debug().Msgf("Ignoring link to file %q: detected content type %v", filepath, contentType)
			return "", nil
		}

		linkContents = bytes.ReplaceAll(
			linkContents,
			[]byte("\r\n"),
			[]byte("\n"),
		)

		// This helps to determine if found link points to file that's
		// not markdown or have mark required metadata
		linkMeta, _, err := metadata.ExtractMeta(linkContents, spaceForLinks, titleFromH1, titleFromFilename, filepath, parents, titleAppendGeneratedHash, "", frontMatterEnabled)
		if err != nil {
			log.Error().
				Err(err).
				Msgf(
					"unable to extract metadata from %q; ignoring the relative link",
					filepath,
				)

			return "", nil
		}

		if linkMeta == nil {
			return "", nil
		}

		log.Trace().
			Msgf(
				"extracted metadata: space=%s title=%s",
				linkMeta.Space,
				linkMeta.Title,
			)

		result, err = getConfluenceLink(api, linkMeta.Space, linkMeta.Title)
		if err != nil {
			return "", fmt.Errorf("find confluence page (file=%s, space=%s, title=%s): %w", filepath, linkMeta.Space, linkMeta.Title, err)
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

// getConfluenceLink builds a stable Confluence tiny link for the given page or blog post.
// Tiny links use the format {baseURL}/x/{encodedPageID} and are immune to
// Cloud-specific URL variations like /ex/confluence/<cloudId>/wiki/...
func getConfluenceLink(
	api *confluence.API,
	space, title string,
) (string, error) {
	// Try to find as a page first
	page, err := api.FindPage(space, title, "page")
	if err != nil {
		return "", fmt.Errorf("api: find page %q in space %q: %w", title, space, err)
	}

	// If not found as a page, try to find as a blog post
	if page == nil {
		page, err = api.FindPage(space, title, "blogpost")
		if err != nil {
			return "", fmt.Errorf("api: find blogpost %q in space %q: %w", title, space, err)
		}
	}

	if page == nil {
		return "", nil
	}

	// Prefer the base URL from the API response (_links.base) as it contains
	// the canonical user-facing wiki URL (e.g., https://tenant.atlassian.net/wiki).
	// Fall back to api.BaseURL if _links.base is not available.
	baseURL := page.Links.Base
	if baseURL == "" {
		baseURL = api.BaseURL
	}

	tiny, err := GenerateTinyLink(baseURL, page.ID)
	if err != nil {
		return "", fmt.Errorf("generate tiny link for page %s: %w", page.ID, err)
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
