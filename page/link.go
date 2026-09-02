package page

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

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

	// SearchDirs are tried after Base, and exist for links written inside an
	// included fragment. Such a link is written from where the fragment lives,
	// not from where it is included, so the fragment's own directory has to be
	// among the places looked. Base is always tried first, so a document's own
	// links keep their meaning.
	SearchDirs []string

	// Checker decides how much is verified. Nil checks nothing.
	Checker *LinkChecker

	// SourceFile is the document being resolved for, so that a link waiting on
	// a page this run has yet to create can name the file to publish again.
	SourceFile string

	// Deferrals collects those waits. Nil says nobody is going to publish
	// anything a second time -- a dry run, or the second pass itself -- so a
	// link to a page that is not there is reported rather than waited on.
	Deferrals *Deferrals

	// broken collects the links that failed a check, rather than the first one
	// stopping the file. Reporting one at a time would mean fixing a link,
	// running again, and finding the next -- which for a page with six of them
	// is six runs to learn what one could have said.
	//
	// No lock: a resolver belongs to one file, and a file is walked by one
	// goroutine.
	broken []string
}

// Broken returns what failed a check, in the order the links appear.
func (r *LinkResolver) Broken() []string {
	if r == nil {
		return nil
	}

	return r.broken
}

// note records a failed check and keeps going.
func (r *LinkResolver) note(format string, args ...any) {
	r.broken = append(r.broken, fmt.Sprintf(format, args...))
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
	checker *LinkChecker,
	searchDirs []string,
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
		Checker:                  checker,
		SearchDirs:               searchDirs,
	}
}

// Resolve reports what a link target should become, or "" to leave it alone.
//
// The target arrives as the document wrote it -- a path with an optional
// #fragment -- which is the same shape the old scanner extracted by hand.
// The text is the words between the brackets, which an ac: link with nothing
// after the colon uses as the page title -- the [Some Page](ac:) form. It is
// wanted for that alone; nothing else looks at it.
func (r *LinkResolver) Resolve(target, text string) (string, error) {
	if r == nil || r.API == nil || target == "" {
		return "", nil
	}

	// A link somewhere else entirely. It is left as written either way; asked
	// whether it answers only when external links are being checked.
	if strings.Contains(target, "://") {
		if err := r.Checker.CheckExternal(target); err != nil {
			r.note("%s: %s", target, err)
		}

		return "", nil
	}

	// A link to a Confluence page by title rather than by path. The renderer
	// turns it into an ac:link; all that can be checked is that there is a page
	// of that name to arrive at.
	if strings.HasPrefix(target, confluenceLinkPrefix) {
		return "", r.checkConfluenceLink(target, text)
	}

	// Neither of these names a file in this repository, and a rooted path is
	// either site-absolute or one an attachment already claimed.
	if strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "/") {
		return "", nil
	}

	filename, hash, _ := strings.Cut(target, "#")

	resolved, why, err := resolveLink(
		r.API, append([]string{r.Base}, r.SearchDirs...),
		markdownLink{full: target, filename: filename, hash: hash},
		r.SpaceForLinks, r.TitleFromH1, r.TitleFromFilename,
		r.Parents, r.TitleAppendGeneratedHash, r.FrontMatterEnabled,
	)
	if err != nil {
		return "", fmt.Errorf("resolve link %q: %w", target, err)
	}

	if why != nil {
		switch {
		case !why.transient:
			if r.checking() {
				r.note("link %q does not resolve: %s", target, why.reason)
			}

		case r.Deferrals != nil:
			// The page is not there yet, and this run is about to create it.
			// Saying so now would be complaining about something that has not
			// finished happening.
			log.Debug().Msgf("link %q waits for %s", target, why.reason)
			r.Deferrals.Note(r.SourceFile)

		case r.checking():
			// Nothing more is going to publish it.
			log.Warn().Msgf("link %q does not resolve: %s", target, why.reason)
		}
	}

	return resolved, nil
}

func (r *LinkResolver) checking() bool {
	return r.Checker != nil && r.Checker.Checks.Internal
}

// confluenceLinkPrefix introduces a link to a page by title.
const confluenceLinkPrefix = "ac:"

// checkConfluenceLink reports whether an ac: link has a page to arrive at.
//
// The title is read the same way the renderer reads it, since a disagreement
// between the two would mean checking one link and publishing another: what
// follows the colon, or the link text when nothing does.
func (r *LinkResolver) checkConfluenceLink(target, text string) error {
	if r.Checker == nil || !r.Checker.Checks.Confluence {
		return nil
	}

	title := strings.TrimSpace(strings.TrimPrefix(target, confluenceLinkPrefix))
	if title == "" {
		title = strings.TrimSpace(text)
	}

	if title == "" {
		r.note("link %q does not resolve: it names no page", target)

		return nil
	}

	// Noted rather than looked up: the page may not have been published yet,
	// and whether it ever is can only be known once the run is over.
	r.Checker.NotePage(r.SpaceForLinks, title, r.Base)

	return nil
}

// unresolved says why a link produced no Confluence link.
//
// Transient marks the one cause that is not the document's fault: the page it
// points at has not been published yet. That is the ordinary state of a first
// run over a repository whose files link to each other, so --check-links
// reports it and carries on rather than failing a build that would succeed on
// the second attempt.
type unresolved struct {
	reason    string
	transient bool
}

// resolveLink reports the Confluence link a target should become.
//
// The second return value says why the answer was empty, in the words a person
// would want to read. Nothing acts on it unless --check-links is on; the rest of
// the time an unresolvable link is left as written, which is what mark has
// always done.
func resolveLink(
	api *confluence.API,
	bases []string,
	link markdownLink,
	spaceForLinks string,
	titleFromH1 bool,
	titleFromFilename bool,
	parents []string,
	titleAppendGeneratedHash bool,
	frontMatterEnabled bool,
) (string, *unresolved, error) {
	var result string

	if len(link.filename) > 0 {
		filepath, why := findLinkTarget(bases, link.filename)
		if why != nil {
			return "", why, nil
		}

		linkContents, err := os.ReadFile(filepath)
		if err != nil {
			return "", nil, fmt.Errorf("read file %s: %w", filepath, err)
		}

		contentType := http.DetectContentType(linkContents)
		// Check if the MIME type starts with "text/"
		if !strings.HasPrefix(contentType, "text/") {
			log.Debug().Msgf("Ignoring link to file %q: detected content type %v", filepath, contentType)
			return "", &unresolved{reason: fmt.Sprintf("it is not a text file (%s)", contentType)}, nil
		}

		linkContents = bytes.ReplaceAll(
			linkContents,
			[]byte("\r\n"),
			[]byte("\n"),
		)

		// This helps to determine if found link points to file that's
		// not markdown or have mark required metadata
		linkMeta, linkBody, err := metadata.ExtractMeta(linkContents, spaceForLinks, titleFromH1, titleFromFilename, filepath, parents, titleAppendGeneratedHash, "", frontMatterEnabled)
		if err != nil {
			log.Error().
				Err(err).
				Msgf(
					"unable to extract metadata from %q; ignoring the relative link",
					filepath,
				)

			return "", &unresolved{reason: "its metadata could not be read"}, nil
		}

		if linkMeta == nil {
			return "", &unresolved{reason: "it has no mark metadata, so it is never published"}, nil
		}

		log.Trace().
			Msgf(
				"extracted metadata: space=%s title=%s",
				linkMeta.Space,
				linkMeta.Title,
			)

		// A file with no headers still gets metadata when the run knows a space,
		// so absence shows up as an empty title rather than a nil. Either way
		// the file is never published and the link can never lead anywhere.
		if linkMeta.Title == "" {
			return "", &unresolved{reason: "it has no title, so it is never published"}, nil
		}

		result, err = getConfluenceLink(api, linkMeta.Space, linkMeta.Title)
		if err != nil {
			return "", nil, fmt.Errorf("find confluence page (file=%s, space=%s, title=%s): %w", filepath, linkMeta.Space, linkMeta.Title, err)
		}

		if result == "" {
			return "", &unresolved{
				reason: fmt.Sprintf(
					"%q is not in space %q yet", linkMeta.Title, linkMeta.Space,
				),
				transient: true,
			}, nil
		}

		// A link naming a section of the other document is written as an
		// anchor rather than as a URL. A fragment on a Confluence URL names
		// nothing: Confluence puts a heading's anchor in the Anchor macro, not
		// in the address, so ".../x/abc123#Setup" scrolls nowhere however the
		// fragment is spelled. ac:link with ri:page and ac:anchor is how the
		// storage format says "that section of that page".
		//
		// Written only once the page has been found, so that a link to a
		// document that is not published yet is still reported and still
		// deferred -- the anchor changes what the link becomes, not whether it
		// resolves.
		//
		// Only within one space: an ri:page with no ri:space-key means this
		// space, while the URL carries its own space in it.
		if link.hash != "" && linkMeta.Space == spaceForLinks {
			if anchor := headingAnchor(linkBody, link.hash); anchor != "" {
				return "ac:" + linkMeta.Title + "#" + anchor, nil, nil
			}
		}
	}

	if len(link.hash) > 0 {
		result = result + "#" + link.hash
	}

	return result, nil, nil
}

// getConfluenceLink builds a stable Confluence tiny link for the given page or blog post.
// Tiny links use the format {baseURL}/x/{encodedPageID} and are immune to
// Cloud-specific URL variations like /ex/confluence/<cloudId>/wiki/...
// findLinkTarget looks for the file a link names, in each base in turn.
//
// The first base is the document's own directory and wins where both would do,
// so adding places to look cannot change what an unambiguous link already meant.
func findLinkTarget(bases []string, name string) (string, *unresolved) {
	var directory bool

	for _, base := range bases {
		candidate := filepath.Join(base, name)

		log.Trace().Msgf("filepath: %s", candidate)
		stat, err := os.Stat(candidate)
		if err != nil {
			// Not a file on disk here, or unreadable. Swallowing err is
			// deliberate: the next base may have it, and if none does the link
			// is left as written rather than failing the run.
			continue //nolint:nilerr
		}

		if stat.IsDir() {
			directory = true
			continue
		}

		return candidate, nil
	}

	if directory {
		return "", &unresolved{reason: "it is a directory, not a document"}
	}

	return "", &unresolved{reason: "there is no such file"}
}

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

// Deferrals remembers which documents linked to pages that did not exist yet.
//
// mark resolves a link by finding the page it points at, so a document linking
// to another that the same run is about to create has nothing to find. Rather
// than leave the link dead until somebody runs mark again -- which is what the
// documentation used to advise -- the documents that waited are published a
// second time once everything exists.
type Deferrals struct {
	mu    sync.Mutex
	files map[string]bool
}

// NewDeferrals returns an empty collector.
func NewDeferrals() *Deferrals {
	return &Deferrals{files: map[string]bool{}}
}

// Note records that a document has a link waiting on a page not yet published.
func (d *Deferrals) Note(file string) {
	if d == nil || file == "" {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.files[file] = true
}

// Files returns the documents to publish again, in a stable order.
func (d *Deferrals) Files() []string {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	files := make([]string, 0, len(d.files))
	for file := range d.files {
		files = append(files, file)
	}
	sort.Strings(files)

	return files
}
