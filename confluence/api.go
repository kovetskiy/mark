package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type User struct {
	AccountID string `json:"accountId,omitempty"`
	UserKey   string `json:"userKey,omitempty"`
	Username  string `json:"username,omitempty"`

	// Email, PublicName and DisplayName are how Cloud names a user now that
	// usernames are gone; the current-user endpoint returns them.
	Email       string `json:"email,omitempty"`
	PublicName  string `json:"publicName,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// isNamed reports whether name is one of the identifiers the user goes by.
func (user *User) isNamed(name string) bool {
	for _, id := range []string{user.Email, user.Username, user.PublicName, user.DisplayName} {
		if id != "" && strings.EqualFold(id, name) {
			return true
		}
	}
	return false
}

type API struct {
	// v1 is rooted at /rest/api and v2 at /api/v2, the newer endpoints such
	// as folders. site is rooted at the base URL itself, for the one web
	// action mark calls; see moveByAction.
	v1      *client
	v2      *client
	site    *client
	BaseURL string

	// username is the login mark authenticates with, empty for a Personal
	// Access Token. Whoever it names is the current user by definition, which
	// restrictionUserCloud relies on when the profile does not repeat it.
	username string

	// gateway is set when the base URL is the api.atlassian.com gateway, the
	// route a scoped API token takes. The page calls go to v2 there, since a
	// scoped token is not entitled to v1; see v2pages.go.
	gateway bool

	// siteBase is the site's own URL as v2 reports it in _links.base, learned
	// from the first v2 listing of the run; see siteBaseURL.
	siteBase      string
	siteBaseMutex sync.RWMutex

	// contentTypesV2 maps a content id to "page" or "blogpost" for every
	// object v2 has handed over, so that a call given only an id can address
	// the right collection; see collectionOfV2.
	contentTypesV2 sync.Map

	// isCloudFlag and isCloudErr are the answer Cloud memoises once
	// isCloudKnown is set, all three guarded by isCloudMutex; see Cloud.
	isCloudFlag  bool
	isCloudErr   error
	isCloudKnown bool
	isCloudMutex sync.Mutex

	// moveEndpointMissing holds the error this Confluence's answer to the v1
	// content move endpoint was reported as, so that a run reorganising a
	// whole tree does not pay a doomed request per page before falling back;
	// see moveContent.
	moveEndpointMissing atomic.Pointer[error]

	// moveActionMissing is the same for /pages/movepage.action; see
	// moveByAction.
	moveActionMissing atomic.Pointer[error]

	// reparented maps a page id to the versions either side of a move this
	// API made by update; see ReparentedFrom.
	reparented      map[string]reparentedVersions
	reparentedMutex sync.Mutex

	pageCache      map[string]*PageInfo
	pageCacheByID  map[string]*PageInfo
	pageCacheMutex sync.RWMutex

	userCache      map[string]userCacheEntry
	userCacheMutex sync.RWMutex

	// currentUser is the answer GetCurrentUser last got, guarded by
	// userCacheMutex. Only a successful lookup is kept, so a failed one is
	// asked again rather than remembered for the rest of the run.
	currentUser *User

	// Both maps are keyed by space key and guarded by the one mutex: they
	// answer the same question about the same thing and are always populated
	// from the same place in a run.
	homePageCache   map[string]homePageCacheEntry
	spaceIDCache    map[string]spaceIDCacheEntry
	spaceCacheMutex sync.RWMutex

	// KeepConcurrentEdits stops UpdatePage from overwriting a page that was
	// written by someone else after mark read it. An update refused as a
	// conflict is then retried only when the page is still at the version mark
	// read, and fails otherwise. --no-overwrite sets it: that flag promises
	// an edit made in Confluence is not silently replaced, and the window
	// between its check and the update is where one could land.
	KeepConcurrentEdits bool

	// conflictRetryDelay is how long UpdatePage waits after a conflict before
	// reading the page again; see retryConflictingUpdate. Tests set it to zero.
	conflictRetryDelay time.Duration

	// moveSettleDelay is how long moveByAction waits between reads of a tree
	// that movepage.action said it changed but does not show it yet.
	moveSettleDelay time.Duration
}

// userCacheEntry records the outcome of a user lookup, including a name that
// matched nobody: a document that mentions an unknown name would otherwise
// re-query for every occurrence, which is the case the cache is least able to
// afford. A lookup that failed for any other reason is not recorded.
type userCacheEntry struct {
	user *User
	err  error
}

// homePageCacheEntry and spaceIDCacheEntry record what a space lookup answered,
// failures included. Neither answer can change while a run is in progress, and
// a space that cannot be resolved cannot become resolvable either -- re-asking
// only pays the same two round trips again for every file in the space.
type homePageCacheEntry struct {
	page *PageInfo
	err  error
}

type spaceIDCacheEntry struct {
	id  string
	err error
}

// pageCacheKey lowercases the title, so every spelling of it shares one
// entry, as it shares one page in Confluence.
func pageCacheKey(space, title, pageType string) string {
	return space + "\x00" + strings.ToLower(title) + "\x00" + pageType
}

func clonePageInfo(page *PageInfo) *PageInfo {
	if page == nil {
		return nil
	}
	cp := *page
	if len(page.Ancestors) > 0 {
		cp.Ancestors = make([]struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}, len(page.Ancestors))
		copy(cp.Ancestors, page.Ancestors)
	}
	return &cp
}

func (api *API) lazyInit() {
	api.pageCacheMutex.RLock()
	if api.pageCache != nil && api.pageCacheByID != nil {
		api.pageCacheMutex.RUnlock()
		return
	}
	api.pageCacheMutex.RUnlock()

	api.pageCacheMutex.Lock()
	defer api.pageCacheMutex.Unlock()
	if api.pageCache == nil {
		api.pageCache = make(map[string]*PageInfo)
	}
	if api.pageCacheByID == nil {
		api.pageCacheByID = make(map[string]*PageInfo)
	}
}

// setCacheEntry sets a cache entry. Requires api.pageCacheMutex to be locked by the caller.
func (api *API) setCacheEntry(key string, page *PageInfo) {
	clonedPage := clonePageInfo(page)
	if oldPage, ok := api.pageCache[key]; ok && oldPage != nil {
		if page == nil || oldPage.ID != page.ID {
			delete(api.pageCacheByID, oldPage.ID)
		}
	}
	api.pageCache[key] = clonedPage
	if clonedPage != nil {
		api.pageCacheByID[clonedPage.ID] = clonedPage
	}
}

func (api *API) invalidatePage(space, title, pageType string) {
	api.lazyInit()
	key := pageCacheKey(space, title, pageType)
	api.pageCacheMutex.Lock()
	if oldPage, ok := api.pageCache[key]; ok && oldPage != nil {
		delete(api.pageCacheByID, oldPage.ID)
	}
	delete(api.pageCache, key)
	api.pageCacheMutex.Unlock()
}

func (api *API) updateCachedPageVersion(id string, newVersion int64) {
	api.lazyInit()
	api.pageCacheMutex.Lock()
	defer api.pageCacheMutex.Unlock()

	var updatedEntry *PageInfo
	if entry, ok := api.pageCacheByID[id]; ok && entry != nil {
		newEntry := clonePageInfo(entry)
		newEntry.Version.Number = newVersion
		api.pageCacheByID[id] = newEntry
		updatedEntry = newEntry
	}

	for key, entry := range api.pageCache {
		if entry != nil && entry.ID == id {
			if updatedEntry != nil {
				api.pageCache[key] = updatedEntry
			} else {
				newEntry := clonePageInfo(entry)
				newEntry.Version.Number = newVersion
				api.pageCache[key] = newEntry
				updatedEntry = newEntry
			}
		}
	}

	if updatedEntry != nil {
		api.pageCacheByID[id] = updatedEntry
	}
}

type SpaceInfo struct {
	ID   int    `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`

	Homepage PageInfo `json:"homepage"`

	Links struct {
		Full string `json:"webui"`
	} `json:"_links"`
}

type PageInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`

	// Status is "current", "archived", "trashed" or "draft".
	//
	// It matters because v1 filters a lookup to current unless told otherwise,
	// so a page being absent from a lookup does not mean its title is free: an
	// archived page is invisible and still owns the name.
	Status string `json:"status"`

	Version struct {
		Number  int64  `json:"number"`
		Message string `json:"message"`
	} `json:"version"`

	Ancestors []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"ancestors"`

	// ParentID and ParentType are the direct parent as v2 names it, and empty
	// for a page read through v1. They exist for the one parent the ancestors
	// cannot show: a folder is not a page, so it is never among them, and a
	// page in a folder looks parentless there. Never sent: v1 has no such
	// fields to read or write.
	ParentID   string `json:"-"`
	ParentType string `json:"-"`

	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`

	Links struct {
		Full string `json:"webui"`
		// Base is the canonical site URL from the response's `_links.base`
		// (e.g. https://tenant.atlassian.net/wiki). CreatePage used to skip
		// this field (`json:"-"`), so same-run tiny links fell back to
		// api.BaseURL and became dead api.atlassian.com URLs.
		Base string `json:"base,omitempty"`
	} `json:"_links"`
}

type AttachmentInfo struct {
	Filename string `json:"title"`
	ID       string `json:"id"`
	Metadata struct {
		Comment string `json:"comment"`
	} `json:"metadata"`
	Links struct {
		Context  string `json:"context"`
		Download string `json:"download"`
	} `json:"_links"`
}

type Label struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
}
type LabelInfo struct {
	Labels []Label `json:"results"`
	Size   int     `json:"number"`
}
type InlineCommentProperties struct {
	OriginalSelection string `json:"originalSelection"`
	MarkerRef         string `json:"markerRef"`
}

type InlineCommentExtensions struct {
	Location         string                  `json:"location"`
	InlineProperties InlineCommentProperties `json:"inlineProperties"`
}

type InlineCommentResult struct {
	Extensions InlineCommentExtensions `json:"extensions"`
}

type InlineComments struct {
	Links struct {
		Context string `json:"context"`
		Next    string `json:"next"`
	} `json:"_links"`
	Results []InlineCommentResult `json:"results"`
}

type FolderInfo struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	Status     string      `json:"status"`
	Title      string      `json:"title"`
	SpaceID    string      `json:"spaceId,omitempty"`
	ParentID   string      `json:"parentId,omitempty"`
	ParentType string      `json:"parentType,omitempty"`
	Position   int         `json:"position,omitempty"`
	AuthorID   string      `json:"authorId,omitempty"`
	OwnerID    string      `json:"ownerId,omitempty"`
	CreatedAt  interface{} `json:"createdAt,omitempty"`
	Version    struct {
		CreatedAt interface{} `json:"createdAt"`
		Message   string      `json:"message"`
		Number    int         `json:"number"`
		MinorEdit bool        `json:"minorEdit"`
		AuthorID  string      `json:"authorId"`
	} `json:"version,omitempty"`
	Links struct {
		Base string `json:"base"`
	} `json:"_links,omitempty"`
}

// form is an attachment upload's multipart body; client.do sends it with the
// headers it needs.
type form struct {
	buffer *bytes.Buffer
	writer *multipart.Writer
}

type tracer struct {
	prefix string
}

func (tracer *tracer) Printf(format string, args ...any) {
	tracer.write(boundTraceDump(fmt.Sprintf(format, args...)))
}

// write traces a message that is already bounded. It is passed on as a
// message rather than as another format string: a dump is arbitrary bytes,
// and a body containing a percent sign would otherwise come out mangled.
//
// Headers are redacted after the bound, so that a large body is not split
// into lines and joined again only for most of it to be thrown away.
func (tracer *tracer) write(message string) {
	log.Trace().Msg(tracer.prefix + " " + redactHeaders(message))
}

// traceDumpLimit is how much of one request or response dump is traced.
//
// A dump is the whole message, body included, and bodies are unbounded: a page
// in storage format, an attachment upload, a proxy's error page. Each one is a
// single log line, and a line of megabytes is no use to anyone reading the log
// while being very expensive for whatever collects it -- one 1 MiB line took a
// GitHub Actions runner over half an hour to ingest. The headers, and the start
// of the body, are what a trace is read for.
const traceDumpLimit = 64 << 10

// boundTraceDump cuts a dump down to traceDumpLimit, on a character boundary,
// and says how much was left out.
func boundTraceDump[T string | []byte](dump T) string {
	return boundTraceDumpMore(dump, 0)
}

// boundTraceDumpMore is boundTraceDump for a dump that more bytes, already
// left out of it, followed.
func boundTraceDumpMore[T string | []byte](dump T, more int64) string {
	if len(dump) <= traceDumpLimit {
		if more > 0 {
			return fmt.Sprintf("%s... (%d more bytes not traced)", dump, more)
		}
		return string(dump)
	}

	cut := traceDumpLimit
	for cut > 0 && !utf8.RuneStart(dump[cut]) {
		cut--
	}

	return fmt.Sprintf("%s... (%d more bytes not traced)", dump[:cut], int64(len(dump)-cut)+more)
}

// sensitiveHeaders name the values that are credentials rather than metadata.
var sensitiveHeaders = []string{
	"authorization",
	"proxy-authorization",
	"cookie",
	"set-cookie",
}

// redactHeaders replaces the value of every credential-bearing header in a raw
// HTTP dump.
//
// The trace is a dump of the whole request, headers included, taken after the
// client has set Authorization -- so it wrote the token, in a form that decodes
// in one step, wherever the log goes. "--log-level TRACE" is the natural thing
// to ask a bug reporter for, which is how it reached issue threads and CI logs,
// and it undid the masking the config dump already does.
//
// Only the value goes. Which headers were sent, and that one carried
// credentials at all, is exactly what someone reading a trace is trying to
// establish.
func redactHeaders(dump string) string {
	lines := strings.Split(dump, "\n")

	for i, line := range lines {
		name, _, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		if !slices.Contains(sensitiveHeaders, strings.ToLower(strings.TrimSpace(name))) {
			continue
		}

		// A dump keeps its CRLFs, and the line endings should survive being
		// read as the HTTP message they are.
		if strings.HasSuffix(line, "\r") {
			lines[i] = name + ": <redacted>\r"
			continue
		}

		lines[i] = name + ": <redacted>"
	}

	return strings.Join(lines, "\n")
}

func NewAPI(baseURL string, username string, password string, insecureSkipVerify bool) *API {
	// Normalize baseURL once before building all derived endpoints.
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Retries happen in the client's retryTransport and nowhere else; see
	// newHTTPClient.
	httpClient := newHTTPClient(insecureSkipVerify)

	trace := func(prefix string) string {
		if zerolog.GlobalLevel() == zerolog.TraceLevel {
			return prefix
		}
		return ""
	}

	return &API{
		v1:            newClient(baseURL+"/rest/api", httpClient, username, password, trace("rest:")),
		v2:            newClient(baseURL+"/api/v2", httpClient, username, password, trace("rest-v2:")),
		site:          newClient(baseURL, httpClient, username, password, trace("site:")),
		BaseURL:       baseURL,
		username:      username,
		gateway:       isGatewayURL(baseURL),
		pageCache:     make(map[string]*PageInfo),
		pageCacheByID: make(map[string]*PageInfo),

		conflictRetryDelay: time.Second,
		moveSettleDelay:    250 * time.Millisecond,
	}
}

// wait sits out d -- the delay before a conflicting write is retried, or
// between reads of a move that has not settled yet -- and returns the
// context's error instead if ctx ends first, marked as a
// cancellation like a request's.
func (api *API) wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	return cancellation(ctx, sleepContext(ctx, d, nil))
}

// FindRootPage returns the page a chain of parents is created under when no
// member of it exists yet: the space's home page.
//
// It used to be whichever page a title-less content listing happened to put
// first, climbed to its outermost ancestor. That is the home page only in a
// space with a single tree. With any other page at the root of the space -- a
// second top-level page, a page moved out from under the home page -- listed
// first, every missing parent was created under that page instead, where
// ValidateAncestry, which compares against the home page, went on to report
// what had just been built as misplaced.
//
// The listing survives only for a space that exists and has no home page,
// where there is nothing better to anchor on and refusing would stop documents
// that published before.
func (api *API) FindRootPage(ctx context.Context, space string) (*PageInfo, error) {
	home, err := api.FindHomePage(ctx, space)
	if err == nil {
		return &PageInfo{
			ID:    home.ID,
			Title: home.Title,
		}, nil
	}
	if !errors.Is(err, errNoHomePage) {
		return nil, fmt.Errorf("can't obtain home page of space %q: %w", space, err)
	}

	log.Warn().Msgf(
		"space %q has no home page; creating missing parents under its first listed root page",
		space,
	)

	return api.firstRootPage(ctx, space)
}

// firstRootPage is FindRootPage's answer for a space without a home page: the
// outermost ancestor of whichever page the space listing returns first.
func (api *API) firstRootPage(ctx context.Context, space string) (*PageInfo, error) {
	page, err := api.FindPage(ctx, space, ``, "page")
	if err != nil {
		return nil, fmt.Errorf("can't obtain first page from space %q: %w", space, err)
	}

	if page == nil {
		// The space exists -- FindHomePage said so -- it is simply empty.
		return nil, fmt.Errorf("space %q has neither a home page nor any page to create parents under", space)
	}

	if len(page.Ancestors) == 0 {
		return &PageInfo{
			ID:    page.ID,
			Title: page.Title,
		}, nil
	}

	return &PageInfo{
		ID:    page.Ancestors[0].ID,
		Title: page.Ancestors[0].Title,
	}, nil
}

// worthCaching reports whether an outcome is one that cannot change during the
// run, and so is safe to remember.
//
// A space's home page and a space's id are both fixed for the life of a run,
// which is what makes memoising them correct. The premise does not extend to
// every way of failing to find them: a 404, or a space that genuinely has no
// home page, is a conclusion, but a throttling burst or a gateway error is a
// moment. The retry transport gives up after four attempts, so remembering one
// of those made a few unlucky seconds fail every remaining file in the space --
// where before the memoisation the next document would simply have asked again.
//
// A cancelled request is never one: it is the caller who stopped asking, and
// an error that wraps a 404 from one call and a cancellation from the next --
// the v1 refusal that sends a lookup on to v2 -- says nothing about the
// answer either.
func worthCaching(err error) bool {
	if IsCancellation(err) {
		return false
	}

	return err == nil ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, errNoHomePage) ||
		errors.Is(err, errNoSuchUser)
}

// errNoHomePage is the answer for a space that exists and has no home page.
// A conclusion rather than a hiccup, so it is remembered like a success.
var errNoHomePage = errors.New("space has no home page")

// errNoSuchUser is the answer for a user search that succeeded and matched
// nobody. Not ErrNotFound: nothing answered 404, and saying so would send
// people looking for a missing endpoint rather than a misspelt name.
var errNoSuchUser = errors.New("no user matches that name")

// FindHomePage returns a space's home page.
//
// Results are cached for the lifetime of the API value, failures included. The
// call is made for every non-blogpost document, again for any page that turns
// out to have no ancestors, and once more when the manifest loads -- and it
// costs two requests rather than one on a scoped token, because the v1 refusal
// is what sends it to v2. A space's home page cannot change mid-run, so asking
// twice can only ever get the same answer.
func (api *API) FindHomePage(ctx context.Context, space string) (*PageInfo, error) {
	if entry, ok := api.cachedHomePage(space); ok {
		return clonePageInfo(entry.page), entry.err
	}

	page, err := api.fetchHomePage(ctx, space)

	if worthCaching(err) {
		api.spaceCacheMutex.Lock()
		if api.homePageCache == nil {
			api.homePageCache = make(map[string]homePageCacheEntry)
		}
		api.homePageCache[space] = homePageCacheEntry{page: clonePageInfo(page), err: err}
		api.spaceCacheMutex.Unlock()
	}

	return page, err
}

func (api *API) cachedHomePage(space string) (homePageCacheEntry, bool) {
	api.spaceCacheMutex.RLock()
	defer api.spaceCacheMutex.RUnlock()
	entry, ok := api.homePageCache[space]
	return entry, ok
}

// spaceFallbackError is how a space lookup fails when v1 did not answer it and
// the v2 fallback, which failed with v2Err, did not either: FindHomePage and
// GetSpaceID ask the same two questions and fail the same way.
//
// v1's answer is the one reported and the one that decides what the failure
// is. Server and Data Center have no /api/v2, so there v2's 404 says nothing
// about the space, and, being a 404, would get a v1 outage cached as a space
// that is not there. v2's is kept for the message only. Through the gateway
// v1 is never asked, and v2's answer stands as it is; so it does when the run
// was stopped during the fallback, since wrapping it would hide the
// cancellation from errors.Is behind a %v, and when v1 answered but with
// nothing to go on. v1's error goes through requestError like any other, so
// that a body that is not JSON is worded with its URL and status.
func (api *API) spaceFallbackError(v1Operation string, v1Err, v2Err error) error {
	if api.gateway || v1Err == nil || IsCancellation(v2Err) {
		return v2Err
	}

	v1Err = requestError(v1Operation, v1Err)

	return fmt.Errorf("v1 API: %w (v2 fallback also failed: %v)", v1Err, v2Err) //nolint:errorlint // v2's 404 on Server must not reach errors.Is(ErrNotFound)
}

func (api *API) fetchHomePage(ctx context.Context, space string) (*PageInfo, error) {
	var v1Err error

	// Through the gateway v1 is not asked at all: a scoped token is refused
	// there, and v2 answers either kind of token.
	if !api.gateway {
		var v1Result SpaceInfo
		v1Result, v1Err = get[SpaceInfo](
			ctx, api.v1, []string{"space", space}, url.Values{"expand": {"homepage"}},
		)
		if v1Err == nil {
			homepage := &v1Result.Homepage

			// A 200 does not guarantee a homepage came with it. Returning the
			// zero PageInfo handed the caller an empty id, which then went out
			// as a content id -- a page published under nothing at all. v2
			// already tells "space has no homepage" apart from "no such space";
			// v1 says the same thing here rather than falling through to a v2
			// that does not exist on Server or Data Center.
			if homepage.ID == "" {
				return nil, fmt.Errorf("space %s: %w", space, errNoHomePage)
			}

			return homepage, nil
		}
	}

	// Any non-OK v1 answer falls through to v2, mirroring GetSpaceID. The case
	// that motivated the fallback is a scoped API token going through the
	// api.atlassian.com gateway: those tokens lack the classic scopes, so the
	// v1 space endpoint answers 404 and every run aborted here. The fallback is
	// deliberately not narrowed to 404: a token with partial scopes can also
	// draw 401/403 from v1 while v2 still answers.
	v2Result, v2Err := get[listing[struct {
		ID         string `json:"id"`
		HomepageID string `json:"homepageId"`
	}]](ctx, api.v2, []string{"spaces"}, url.Values{"keys": {space}})
	v2Err = requestError("look up space "+space, v2Err)

	// When v2 cannot answer either, report why v1 refused rather than why v2
	// did. Server/DC has no /api/v2 at all, so every fallback there ends in a
	// 404 from a path that was never going to exist, and surfacing that instead
	// of v1's answer turns a clear "401 (Unauthorized)" -- the error a Server
	// user with bad credentials should see -- into a misleading "404".
	//
	// v1's answer is also the only one that decides what the failure is. That
	// 404 from a v2 that does not exist used to be wrapped too, so a v1 outage
	// (503, 429) or a 401 matched ErrNotFound, and FindHomePage cached it as a
	// space that is not there for the rest of the run. Through the gateway v1
	// is never asked and v2's answer is the only one, so it stands as it is.
	if v2Err != nil {
		return nil, api.spaceFallbackError("read space "+space, v1Err, v2Err)
	}

	api.learnSiteBase(v2Result.Links.Base)

	if len(v2Result.Results) == 0 {
		return nil, fmt.Errorf("space with key %s not found: %w", space, ErrNotFound)
	}

	// A space that exists but has no homepage is a different failure from a
	// space that does not exist, and reporting it as "not found" sends people
	// looking for a typo in a space key that is perfectly correct.
	if v2Result.Results[0].HomepageID == "" {
		return nil, fmt.Errorf("space %s: %w", space, errNoHomePage)
	}

	return api.GetPageByID(ctx, v2Result.Results[0].HomepageID)
}

func (api *API) FindPage(
	ctx context.Context,
	space string,
	title string,
	pageType string,
) (*PageInfo, error) {
	api.lazyInit()
	key := pageCacheKey(space, title, pageType)
	api.pageCacheMutex.RLock()
	if page, ok := api.pageCache[key]; ok {
		cloned := clonePageInfo(page)
		api.pageCacheMutex.RUnlock()
		return cloned, nil
	}
	api.pageCacheMutex.RUnlock()

	var (
		found *PageInfo
		err   error
	)
	if api.gateway {
		found, err = api.findPageV2(ctx, space, title, pageType, "current")
	} else {
		found, err = api.findPageV1(ctx, space, title, pageType)
	}
	if err != nil {
		return nil, err
	}

	api.pageCacheMutex.Lock()
	defer api.pageCacheMutex.Unlock()

	// Double-checked locking: check if the cache was populated by another goroutine
	// while the network request was in-flight.
	if cachedPage, ok := api.pageCache[key]; ok {
		return clonePageInfo(cachedPage), nil
	}

	if found == nil {
		api.setCacheEntry(key, nil)
		return nil, nil
	}

	page := *found

	cacheTitle := title
	if page.Title != "" {
		cacheTitle = page.Title
	}
	cacheType := pageType
	if page.Type != "" {
		cacheType = page.Type
	}
	canonicalKey := pageCacheKey(space, cacheTitle, cacheType)

	api.setCacheEntry(key, &page)
	if canonicalKey != key {
		api.setCacheEntry(canonicalKey, &page)
	}
	return clonePageInfo(&page), nil
}

// findPageV1 is the lookup behind FindPage: a live page by title, or nil.
func (api *API) findPageV1(ctx context.Context, space, title, pageType string) (*PageInfo, error) {
	query := url.Values{
		"spaceKey": {space},
		"expand":   {"ancestors,version"},
		"type":     {pageType},
		// Stated rather than left to the default, because the default is the
		// whole subtlety here: this lookup deliberately sees live content only,
		// and pageHoldingTitle is the one place that asks about the rest.
		"status": {"current"},
	}

	if title != "" {
		query.Set("title", title)
	}

	result, err := get[listing[PageInfo]](ctx, api.v1, []string{"content", ""}, query)
	// allow 404 because it's fine if page is not found,
	// the function will return nil, nil
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, requestError(fmt.Sprintf("find page %q in space %s", title, space), err)
	}

	if len(result.Results) == 0 {
		return nil, nil
	}

	page := result.Results[0]
	// Populate the base URL from the response _links.base or fallback to BaseURL
	if result.Links.Base != "" {
		page.Links.Base = result.Links.Base
	} else if page.Links.Base == "" {
		page.Links.Base = api.BaseURL
	}

	return &page, nil
}

// statusesHoldingTitle are the states in which a page is invisible to an
// ordinary lookup and still owns its title.
//
// Whether Confluence really refuses to reuse the title of an archived or
// trashed page is not verifiable from here without an instance. Everything
// below only ever produces a better error message on a create that has already
// failed, so being wrong about it costs a misleading sentence rather than a
// wrong action.
var statusesHoldingTitle = []string{"archived", "trashed"}

// findPageWithStatus looks a title up in one particular status.
//
// Separate from FindPage rather than a parameter on it: the page cache is keyed
// by space, title and type, with no room for a fourth dimension, and caching an
// archived page under the key a live one will want would be worse than not
// caching at all. This is made only after a create has failed, so paying for it
// each time is fine.
func (api *API) findPageWithStatus(ctx context.Context, space, title, pageType, status string) (*PageInfo, error) {
	if api.gateway {
		return api.findPageV2(ctx, space, title, pageType, status)
	}

	query := url.Values{
		"spaceKey": {space},
		"expand":   {"ancestors,version"},
		"type":     {pageType},
		"status":   {status},
	}

	if title != "" {
		query.Set("title", title)
	}

	result, err := get[listing[PageInfo]](ctx, api.v1, []string{"content", ""}, query)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, requestError(fmt.Sprintf("find %s page %q in space %s", status, title, space), err)
	}

	if len(result.Results) == 0 {
		return nil, nil
	}

	page := result.Results[0]
	if page.Status == "" {
		page.Status = status
	}

	return &page, nil
}

// pageHoldingTitle finds a page that owns a title without being visible to an
// ordinary lookup, or nil.
//
// This is the gap that made --on-orphan archive a one-way door: mark archives a
// page, the source file comes back, FindPage returns nil because v1 hides
// archived content, and the create that follows fails with a bare
// 400 Bad Request naming nothing.
func (api *API) pageHoldingTitle(ctx context.Context, space, title, pageType string) *PageInfo {
	for _, status := range statusesHoldingTitle {
		page, err := api.findPageWithStatus(ctx, space, title, pageType, status)
		if err != nil || page == nil {
			// The create has already failed. A failure to explain it is not
			// worth replacing the caller's real error with.
			continue
		}
		return page
	}

	return nil
}

// explainCreateFailure adds the one cause of a failed create the API never
// names, and leaves every other failure exactly as it was.
//
// It reports rather than acts: restoring or purging somebody's page is a
// decision for a person, and mark has already shown it cannot see the whole
// picture here.
func (api *API) explainCreateFailure(ctx context.Context, space, title, pageType string, err error) error {
	blocker := api.pageHoldingTitle(ctx, space, title, pageType)
	if blocker == nil {
		return err
	}

	return fmt.Errorf(
		"%w -- a page titled %q exists in space %s but is %s (id %s), and it still "+
			"holds the title; restore or purge it in Confluence, or publish under a "+
			"different title",
		err, title, space, blocker.Status, blocker.ID,
	)
}

func (api *API) CreateAttachment(
	ctx context.Context,
	pageID string,
	name string,
	comment string,
	reader io.Reader,
) (AttachmentInfo, error) {
	var info AttachmentInfo

	form, err := getAttachmentPayload(name, comment, reader)
	if err != nil {
		return AttachmentInfo{}, err
	}

	result, err := send[listing[AttachmentInfo]](
		ctx, api.v1, http.MethodPost, []string{"content", pageID, "child", "attachment"}, form,
	)
	if err != nil {
		return info, requestError(fmt.Sprintf("attach %q to page %s", name, pageID), err)
	}

	if len(result.Results) == 0 || result.Results[0].ID == "" {
		return info, errors.New(
			"the Confluence REST API for creating attachments returned " +
				"no attachment, expected one with an id",
		)
	}

	withListingContext(&result)

	return result.Results[0], nil
}

// withListingContext gives each attachment in a listing the listing's context
// path where it names none of its own: the attachment's download link is
// relative to it, and a listing says it once for all of its results.
func withListingContext(attachments *listing[AttachmentInfo]) {
	for i := range attachments.Results {
		if attachments.Results[i].Links.Context == "" {
			attachments.Results[i].Links.Context = attachments.Links.Context
		}
	}
}

// UpdateAttachment uploads a new version of the same attachment if the
// checksums differs from the previous one.
// It also handles a case where Confluence returns sort of "short" variant of
// the response instead of an extended one.
func (api *API) UpdateAttachment(
	ctx context.Context,
	pageID string,
	attachID string,
	name string,
	comment string,
	reader io.Reader,
) (AttachmentInfo, error) {
	var info AttachmentInfo

	form, err := getAttachmentPayload(name, comment, reader)
	if err != nil {
		return AttachmentInfo{}, err
	}

	result, err := send[json.RawMessage](
		ctx, api.v1, http.MethodPost,
		[]string{"content", pageID, "child", "attachment", attachID, "data"}, form,
	)
	if err != nil {
		return info, requestError(fmt.Sprintf("upload a new version of attachment %q of page %s", name, pageID), err)
	}

	var extendedResponse listing[AttachmentInfo]
	err = json.Unmarshal(result, &extendedResponse)
	if err != nil {
		return info, fmt.Errorf("unable to unmarshal JSON response as full response format (JSON=%s): %w", string(result), err)
	}

	if len(extendedResponse.Results) > 0 {
		withListingContext(&extendedResponse)
		info = extendedResponse.Results[0]
	} else if err = json.Unmarshal(result, &info); err != nil {
		return AttachmentInfo{}, fmt.Errorf("unable to unmarshal JSON response as short response format (JSON=%s): %w", string(result), err)
	}

	// The caller links the page's attachments from this answer, so one that
	// names no attachment cannot count as an upload.
	if info.ID == "" {
		return AttachmentInfo{}, fmt.Errorf(
			"the Confluence REST API for updating attachments returned "+
				"no attachment (JSON=%s), expected one with an id",
			string(result),
		)
	}

	return info, nil
}

func getAttachmentPayload(name, comment string, reader io.Reader) (*form, error) {
	var (
		payload = bytes.NewBuffer(nil)
		writer  = multipart.NewWriter(payload)
	)

	content, err := writer.CreateFormFile("file", name)
	if err != nil {
		return nil, fmt.Errorf("unable to create form file: %w", err)
	}

	_, err = io.Copy(content, reader)
	if err != nil {
		return nil, fmt.Errorf("unable to copy i/o between form-file and file: %w", err)
	}

	commentWriter, err := writer.CreateFormField("comment")
	if err != nil {
		return nil, fmt.Errorf("unable to create form field for comment: %w", err)
	}

	_, err = commentWriter.Write([]byte(comment))
	if err != nil {
		return nil, fmt.Errorf("unable to write comment in form-field: %w", err)
	}

	// Always a minor edit. Without the field Cloud treats every upload as a
	// change worth announcing, so a run that touched five diagrams sent
	// watchers five notifications on top of the page's own -- and --minor-edit
	// silenced only the page's. The page update is the change a watcher is
	// told about, and --minor-edit is what decides whether they are; the files
	// it carries are part of that change, not separate news. Server and Data
	// Center document the same field.
	err = writer.WriteField("minorEdit", "true")
	if err != nil {
		return nil, fmt.Errorf("unable to write minorEdit in form-field: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("unable to close form-writer: %w", err)
	}

	return &form{
		buffer: payload,
		writer: writer,
	}, nil
}

func (api *API) GetAttachments(ctx context.Context, pageID string) ([]AttachmentInfo, error) {
	if api.gateway {
		return api.getAttachmentsV2(ctx, pageID)
	}

	return listV1(
		ctx, api, []string{"content", pageID, "child", "attachment"},
		url.Values{"expand": {"version,container"}}, 100,
		"list attachments of page "+pageID, false, withListingContext,
	)
}

func (api *API) GetPageByID(ctx context.Context, pageID string) (*PageInfo, error) {
	return api.GetPageByIDExpanded(ctx, pageID, "ancestors,version")
}

func (api *API) GetPageByIDExpanded(ctx context.Context, pageID string, expand string) (*PageInfo, error) {
	if api.gateway {
		return api.getPageByIDV2(ctx, pageID, expand)
	}

	page, err := get[PageInfo](ctx, api.v1, []string{"content", pageID}, url.Values{"expand": {expand}})
	if err != nil {
		return nil, requestError("read page "+pageID, err)
	}

	return &page, nil
}

func (api *API) GetInlineComments(ctx context.Context, pageID string) (*InlineComments, error) {
	all := &InlineComments{}

	results, err := listV1(
		ctx, api, []string{"content", pageID, "child", "comment"},
		url.Values{"expand": {"extensions.inlineProperties"}}, 100,
		"read inline comments of page "+pageID, false,
		func(page *listing[InlineCommentResult]) {
			if all.Links.Context == "" {
				all.Links.Context, all.Links.Next = page.Links.Context, page.Links.Next
			}
		},
	)
	if err != nil {
		return nil, err
	}

	all.Results = results

	return all, nil
}

func (api *API) CreatePage(
	ctx context.Context,
	space string,
	pageType string,
	parent *PageInfo,
	title string,
	body string,
) (*PageInfo, error) {
	var (
		page *PageInfo
		err  error
	)
	if api.gateway {
		parentID := ""
		if parent != nil {
			parentID = parent.ID
		}
		page, err = api.createPageV2(ctx, space, pageType, parentID, "", title, body)
	} else {
		page, err = api.createPageV1(ctx, space, pageType, parent, title, body)
	}
	if err != nil {
		return nil, err
	}

	if parent != nil {
		ancestors := make([]struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}, len(parent.Ancestors)+1)

		for i, a := range parent.Ancestors {
			ancestors[i] = struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			}{
				ID:    a.ID,
				Title: a.Title,
			}
		}
		ancestors[len(parent.Ancestors)] = struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{
			ID:    parent.ID,
			Title: parent.Title,
		}
		page.Ancestors = ancestors
	}

	api.rememberCreatedPage(space, pageType, title, page)

	return page, nil
}

// rememberCreatedPage puts a page just created into the page cache, so the
// lookups later in the run find it without asking.
func (api *API) rememberCreatedPage(space, pageType, title string, page *PageInfo) {
	cacheTitle := title
	if page.Title != "" {
		cacheTitle = page.Title
	}
	cacheType := pageType
	if page.Type != "" {
		cacheType = page.Type
	}
	key := pageCacheKey(space, cacheTitle, cacheType)

	if page.Links.Base == "" {
		page.Links.Base = api.BaseURL
	}

	api.lazyInit()
	api.pageCacheMutex.Lock()
	api.setCacheEntry(key, page)
	api.pageCacheMutex.Unlock()
}

// createPageV1 is the request behind CreatePage.
func (api *API) createPageV1(ctx context.Context, space, pageType string, parent *PageInfo, title, body string) (*PageInfo, error) {
	payload := map[string]any{
		"type":  pageType,
		"title": title,
		"space": map[string]any{
			"key": space,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"representation": "storage",
				"value":          body,
			},
		},
		"metadata": map[string]any{
			"properties": map[string]any{
				"editor": map[string]any{
					"value": "v2",
				},
			},
		},
	}

	if parent != nil {
		payload["ancestors"] = []map[string]any{
			{"id": parent.ID},
		}
	}

	page, err := send[PageInfo](ctx, api.v1, http.MethodPost, []string{"content", ""}, payload)
	if statusOf(err) != 0 {
		return nil, api.explainCreateFailure(ctx, space, title, pageType, err)
	}
	if err != nil {
		return nil, requestError(fmt.Sprintf("create page %q in space %s", title, space), err)
	}

	return &page, nil
}

func (api *API) UpdatePage(ctx context.Context, page *PageInfo, newContent string, minorEdit bool, versionMessage string, appearance string, emojiString string) error {
	nextPageVersion := page.Version.Number + 1

	properties := map[string]any{}

	// A page edited by id with no metadata in the file has no appearance to
	// carry, and an empty value here would reset the width of a page nobody
	// asked to restyle.
	if appearance != "" {
		// Fix to set full-width as has changed on Confluence APIs again.
		// https://jira.atlassian.com/browse/CONFCLOUD-65447
		//
		properties["content-appearance-published"] = map[string]any{
			"value": appearance,
		}
		// The draft key was previously left alone, because the user's editor
		// defaults drove it and writing it produced sporadic published widths
		// (the same CONFCLOUD-65447 above). Writing published on its own,
		// though, leaves the two keys disagreeing, and the editor believes the
		// draft: the width reverts as soon as anyone opens the page. The
		// Confluence editor itself writes both keys with the same value.
		properties["content-appearance-draft"] = map[string]any{
			"value": appearance,
		}
	}

	if emojiString != "" {
		r, size := utf8.DecodeRuneInString(emojiString)
		if r == utf8.RuneError && size <= 1 {
			return fmt.Errorf("invalid UTF-8 in emoji: %q", emojiString)
		}
		unicodeHex := fmt.Sprintf("%x", r)

		properties["emoji-title-draft"] = map[string]any{
			"value": unicodeHex,
		}
		properties["emoji-title-published"] = map[string]any{
			"value": unicodeHex,
		}
	}

	put := func(version int64) error {
		if api.gateway {
			return api.updatePageV2(ctx, page, newContent, minorEdit, versionMessage, version)
		}
		return api.updatePageV1(ctx, page, newContent, minorEdit, versionMessage, version, properties)
	}

	err := put(nextPageVersion)
	if errors.Is(err, ErrConflict) {
		nextPageVersion, err = api.retryConflictingUpdate(ctx, page, nextPageVersion, err, put)
	}
	if err != nil {
		return err
	}

	page.Version.Number = nextPageVersion
	api.updateCachedPageVersion(page.ID, nextPageVersion)

	// An update can carry a new title, and the cache is keyed by title. A
	// lookup of the new title earlier in the same run -- before the page wore
	// it -- cached a miss, and that miss now outlives the fact that produced
	// it: the next caller is told the page does not exist and tries to create
	// one, which Confluence rejects as a duplicate title. Dropping the entry
	// costs one lookup and keeps the cache from asserting something untrue.
	api.forgetMissesForTitle(page.Title, page.Type)

	// v2 keeps properties on an endpoint of their own, so they are written
	// only once the content is. The version is bumped first: the new one
	// exists the moment the content is in, and a caller holding the old
	// number after a failed property write would have its next update refused
	// as a conflict.
	//
	// And they are given the same grace after the run is stopped as the
	// content was: they are the rest of this update, which on v1 goes in the
	// one request. Cut short by a signal, the update would fail after its
	// version exists, so the caller would not record it -- and under
	// --no-overwrite the page would read as edited in Confluence on every
	// later run. With none to write there is nothing to ask, and the listing
	// is not read at all.
	if api.gateway && len(properties) > 0 {
		rest, release := withGrace(ctx, nil)
		defer release()

		return api.setPagePropertiesV2(rest, page, properties)
	}

	return nil
}

// retryConflictingUpdate recovers from an update Confluence refused with 409.
//
// UpdatePage sends the version after the one the page had when it was read,
// and that number goes stale whenever something else writes to the page in
// the meantime: a person in the web UI, another mark run, or a page resolved
// twice in one run through two copies. Confluence Cloud has also been seen to
// refuse the first update of a page created a moment earlier, with an
// OptimisticLockException and a version that was in fact right (issue #139) --
// a race inside Confluence rather than a stale number. Both clear the same
// way: wait a moment, read the version the page has now, and send once more.
//
// mark's content is authoritative for the pages it publishes, so the retry
// overwrites whatever the other writer left, unless KeepConcurrentEdits says
// otherwise. There is exactly one retry: a page that conflicts twice is being
// written to by something else continuously, and racing it is not a fix.
//
// It returns the version the update created.
func (api *API) retryConflictingUpdate(
	ctx context.Context,
	page *PageInfo,
	attempted int64,
	conflict error,
	put func(version int64) error,
) (int64, error) {
	if err := api.wait(ctx, api.conflictRetryDelay); err != nil {
		return 0, fmt.Errorf(
			"update of page %q (%s) conflicted (%w), and was not retried: %w",
			page.Title, page.ID, conflict, err,
		)
	}

	current, err := api.currentPageVersion(ctx, page)
	if err != nil {
		return 0, fmt.Errorf(
			"update of page %q (%s) conflicted (%w), and its current version could not be read to retry: %w",
			page.Title, page.ID, conflict, err,
		)
	}

	if api.KeepConcurrentEdits && current != page.Version.Number {
		return 0, fmt.Errorf(
			"page %q (%s) was edited in Confluence while mark was "+
				"publishing it (now at version %d, mark read version %d), and "+
				"--no-overwrite leaves such a page alone: %w",
			page.Title, page.ID, current, page.Version.Number, conflict,
		)
	}

	log.Debug().Msgf(
		"update of page %q (%s) as version %d conflicted; retrying as version %d",
		page.Title, page.ID, attempted, current+1,
	)

	if err := put(current + 1); err != nil {
		if errors.Is(err, ErrConflict) {
			return 0, fmt.Errorf(
				"version %d of page %q (%s) was refused as a "+
					"conflict, and version %d again after reading the page afresh; "+
					"something else is writing to the page at the same time, so run "+
					"mark again once it has stopped: %w",
				attempted, page.Title, page.ID, current+1, err,
			)
		}
		return 0, err
	}

	return current + 1, nil
}

// currentPageVersion reads the version a page has right now, bypassing the
// cache, with the cheapest request that returns it.
func (api *API) currentPageVersion(ctx context.Context, page *PageInfo) (int64, error) {
	if api.gateway {
		content, err := api.readContentV2(ctx, v2Collection(page.Type), page.ID, false)
		if err != nil {
			return 0, err
		}
		return content.Version.Number, nil
	}

	current, err := api.GetPageByIDExpanded(ctx, page.ID, "version")
	if err != nil {
		return 0, err
	}
	return current.Version.Number, nil
}

// updatePageV1 is the request behind UpdatePage. properties travel inside it,
// as metadata; nextVersion is the version this update creates.
func (api *API) updatePageV1(
	ctx context.Context,
	page *PageInfo,
	newContent string,
	minorEdit bool,
	versionMessage string,
	nextVersion int64,
	properties map[string]any,
) error {
	payload := map[string]any{
		"id":    page.ID,
		"type":  page.Type,
		"title": page.Title,
		"version": map[string]any{
			"number":    nextVersion,
			"minorEdit": minorEdit,
			"message":   versionMessage,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          newContent,
				"representation": "storage",
			},
		},
		"metadata": map[string]any{
			"properties": properties,
		},
	}

	// ancestors on an update is how Confluence documents *moving* a page, so
	// the key goes in only when there is an ancestor to name. A page created
	// under a folder comes back from v2 with no ancestors at all -- folders are
	// not ancestors -- and the update a moment later used to send
	// "ancestors": [], which is at best ignored and at worst read as a request
	// to reparent the page to the space root.
	if page.Type != "blogpost" && len(page.Ancestors) > 0 {
		// picking only the last one, which is required by confluence
		payload["ancestors"] = []map[string]any{
			{"id": page.Ancestors[len(page.Ancestors)-1].ID},
		}
	}

	_, err := api.v1.do(
		ctx, nil, http.MethodPut, []string{"content", page.ID}, nil, payload, &struct{}{},
	)

	return api.confirmVersion(
		ctx, page, nextVersion,
		requestError(fmt.Sprintf("update page %q (%s)", page.Title, page.ID), err),
	)
}

// confirmVersion is the error a write that creates version of page ended
// with, err, unless that is an answer that is not Confluence's -- a 2xx that
// is not JSON, and not certainly a login page -- and the page is found at
// version after all. Then the write was applied, whatever answered it, and
// failing it would leave the version it created unrecorded: under
// --no-overwrite, read as an edit made in Confluence on every later run.
//
// A login page that is only told by its HTML is checked the same way. Behind
// an SSO proxy that is all one is, and the write did not happen; the page is
// then not at version, or cannot be read either, and err stands.
func (api *API) confirmVersion(ctx context.Context, page *PageInfo, version int64, err error) error {
	if !unreadableAnswer(err) {
		return err
	}

	settled, release := withGrace(ctx, nil)
	defer release()

	current, readErr := api.currentPageVersion(settled, page)
	if readErr != nil || current != version {
		return err
	}

	log.Warn().Err(err).Msgf(
		"page %q (%s) is at version %d, the one the write created, so it was applied; "+
			"something in front of Confluence answered it instead",
		page.Title, page.ID, version,
	)

	return nil
}

// forgetMissesForTitle drops cached "no such page" answers for a title.
//
// The cache is keyed by title, and a lookup of a title before any page wore it
// records a miss. An update that moves a page onto that title outlives the fact
// the miss was recorded from: the next caller is told the page does not exist,
// tries to create one, and Confluence rejects the duplicate title.
//
// Only misses are dropped. A cached page filed under a title it no longer
// carries is a different and older question, and one this is not the place to
// answer. The space is not part of the match because PageInfo does not carry
// one; over-invalidating costs a lookup, and under-invalidating costs a failed
// publish.
func (api *API) forgetMissesForTitle(title, pageType string) {
	api.lazyInit()
	api.pageCacheMutex.Lock()
	defer api.pageCacheMutex.Unlock()

	suffix := pageCacheKey("", title, pageType)
	for key, entry := range api.pageCache {
		if entry == nil && strings.HasSuffix(key, suffix) {
			delete(api.pageCache, key)
		}
	}
}

func (api *API) AddPageLabels(ctx context.Context, page *PageInfo, newLabels []string) (*LabelInfo, error) {

	labels := []map[string]any{}
	for _, label := range newLabels {
		if label != "" {
			item := map[string]any{
				"prefix": "global",
				"name":   label,
			}
			labels = append(labels, item)
		}
	}

	payload := labels

	result, err := send[LabelInfo](ctx, api.v1, http.MethodPost, []string{"content", page.ID, "label"}, payload)
	if err != nil {
		return nil, requestError("add labels to page "+page.ID, err)
	}

	return &result, nil
}

func (api *API) DeletePageLabel(ctx context.Context, page *PageInfo, label string) error {
	// The answer is decoded only to hold it to being JSON; nothing in it is
	// used.
	_, err := api.v1.do(
		ctx, nil, http.MethodDelete, []string{"content", page.ID, "label"},
		url.Values{"name": {label}}, nil, &struct{}{},
	)

	return requestError(fmt.Sprintf("remove label %q from page %s", label, page.ID), err)
}

func (api *API) GetPageLabels(ctx context.Context, page *PageInfo, prefix string) (*LabelInfo, error) {
	if api.gateway {
		return api.getPageLabelsV2(ctx, page, prefix)
	}

	all, err := listV1[Label](
		ctx, api, []string{"content", page.ID, "label"},
		url.Values{"prefix": {prefix}}, 50,
		"read labels of page "+page.ID, false, nil,
	)
	if err != nil {
		return nil, err
	}

	return &LabelInfo{Labels: all, Size: len(all)}, nil
}

// GetUserByName resolves a display name to a Confluence user.
//
// Results are cached for the lifetime of the API value. Every @mention in a
// document renders through the "user" stdlib template func, so an uncached
// lookup meant one CQL search per occurrence -- a document naming the same
// person twenty times issued twenty searches, multiplied by every file in the
// run. Names do not change mid-run, so the lookup is memoised, and so is a
// search that matched nobody.
//
// A search that failed is not: the same rule as the space lookups, through
// worthCaching. A throttling burst or a gateway error while resolving the first
// mention of someone otherwise left every later mention of them, in every
// remaining file, failing with an error Confluence had long since stopped
// giving.
func (api *API) GetUserByName(ctx context.Context, name string) (*User, error) {
	if entry, ok := api.cachedUser(name); ok {
		return entry.user, entry.err
	}

	user, err := api.fetchUserByName(ctx, name)

	if !worthCaching(err) {
		return user, err
	}

	api.userCacheMutex.Lock()
	if api.userCache == nil {
		api.userCache = make(map[string]userCacheEntry)
	}
	api.userCache[name] = userCacheEntry{user: user, err: err}
	api.userCacheMutex.Unlock()

	return user, err
}

func (api *API) cachedUser(name string) (userCacheEntry, bool) {
	api.userCacheMutex.RLock()
	defer api.userCacheMutex.RUnlock()
	entry, ok := api.userCache[name]
	return entry, ok
}

func (api *API) fetchUserByName(ctx context.Context, name string) (*User, error) {
	var result struct {
		Results []struct {
			User User
		}
	}

	query := url.Values{"cql": {fmt.Sprintf("user.fullname~%q", name)}}

	// Try the new path first
	_, err := api.v1.do(ctx, nil, http.MethodGet, []string{"search", "user"}, query, nil, &result)
	if err != nil && statusOf(err) == 0 {
		return nil, requestError(fmt.Sprintf("look up user %q", name), err)
	}

	// Try old path
	if err != nil || len(result.Results) == 0 {
		_, err = api.v1.do(ctx, nil, http.MethodGet, []string{"search"}, query, nil, &result)
		if err != nil {
			return nil, requestError(fmt.Sprintf("look up user %q", name), err)
		}
	}

	if len(result.Results) == 0 {

		return nil, fmt.Errorf("user with name %q is not found: %w", name, errNoSuchUser)
	}

	return &result.Results[0].User, nil
}

// GetCurrentUser returns the user mark authenticates as. The answer does not
// change during a run, so a successful lookup is reused by every later call:
// the platform probe and an edit lock both need it.
func (api *API) GetCurrentUser(ctx context.Context) (*User, error) {
	api.userCacheMutex.RLock()
	cached := api.currentUser
	api.userCacheMutex.RUnlock()
	if cached != nil {
		user := *cached
		return &user, nil
	}

	user, err := get[User](ctx, api.v1, []string{"user", "current"}, nil)
	if err != nil {
		return nil, requestError("read the current user", err)
	}

	stored := user
	api.userCacheMutex.Lock()
	api.currentUser = &stored
	api.userCacheMutex.Unlock()

	return &user, nil
}

// isCloudHost reports whether a host is a known Confluence Cloud host, without
// issuing a request.
//
// Matching is on a dot boundary rather than a bare suffix: strings.HasSuffix on
// "atlassian.net" also accepts "notatlassian.net", which would send a
// self-hosted instance down the Cloud code paths.
//
// api.atlassian.com is the gateway that scoped API tokens must go through
// (https://api.atlassian.com/ex/confluence/<cloudId>/wiki). It always fronts
// Cloud, and matching it here means the common scoped-token setup answers from
// the fast path instead of paying for a probe.
func isCloudHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))

	if host == "api.atlassian.com" {
		return true
	}

	for _, domain := range []string{"jira.com", "atlassian.net"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}

	return false
}

// IsCloud reports whether the target is Confluence Cloud, deciding at most once
// per API value. A target that could not be identified is reported as not
// Cloud; Cloud says so as an error, for a caller that must not guess.
func (api *API) IsCloud(ctx context.Context) bool {
	isCloud, _ := api.Cloud(ctx)

	return isCloud
}

// Cloud is IsCloud for a caller that must not guess: it says when the target
// could not be identified, as an error, rather than answering false. A probe
// cut short by its context is reported as such -- IsCancellation recognises
// the error, which wraps the context's own -- and, unlike any other failure,
// is not remembered.
//
// It identifies the target at most once per API value: a known Cloud
// host answers without a request, otherwise the current user decides --
// Cloud names it by an Atlassian accountId, Server and Data Center by a
// username and userKey instead.
//
// Memoised under a mutex held for the whole probe, not a plain bool pair: the
// slow path issues a request, so racing callers would both ask and write the
// result concurrently, and the mutex makes a caller arriving mid-request wait
// for the answer instead of reading a half-written one.
//
// The probe runs with the ctx of the call that finds the answer unknown.
//
// Not a sync.Once, because a probe cut short by its context is not an
// answer, and Once would remember it as "could not identify" for the rest of
// the API's life: an API reused after a cancelled ProcessFileContext would
// take Cloud for Server from then on. A cancelled probe answers for that call
// only, and the next call asks again.
func (api *API) Cloud(ctx context.Context) (bool, error) {
	api.isCloudMutex.Lock()
	defer api.isCloudMutex.Unlock()

	if api.isCloudKnown {
		return api.isCloudFlag, api.isCloudErr
	}

	if api.gateway || isCloudHost(api.v1.hostname()) {
		api.isCloudFlag, api.isCloudKnown = true, true
		return true, nil
	}

	var isCloud bool
	user, err := api.GetCurrentUser(ctx)
	if err != nil {
		err = fmt.Errorf("unable to identify the Confluence platform: %w", err)
	} else {
		isCloud, err = identifyCloud(user)
	}
	if IsCancellation(err) {
		return false, err
	}
	if err != nil {
		log.Warn().Err(err).Msg("unable to tell Confluence Cloud from Server or Data Center; assuming it is not Cloud")
	}

	api.isCloudFlag, api.isCloudErr, api.isCloudKnown = isCloud, err, true

	return isCloud, err
}

// identifyCloud reads the platform off the current user.
func identifyCloud(user *User) (bool, error) {
	switch {
	case user.AccountID != "":
		return true, nil
	case user.Username != "" || user.UserKey != "":
		return false, nil
	default:
		return false, errors.New(
			"unable to identify the Confluence platform: the current user has neither an accountId nor a username",
		)
	}
}

// restrictionUserCloud resolves the user a Cloud page is restricted to.
//
// Cloud restrictions take an accountId, so the name has to be looked up. The
// authenticated user is checked first: --edit-lock passes the configured
// username, which on Cloud is the account's email, and the user search matches
// full names, so it would not find it. The login itself is matched against the
// credential mark authenticated with rather than against the profile: Cloud's
// profile-visibility settings can hide the email from /user/current, and an
// alias login is not the address the profile reports, yet either way the login
// names the current user. Anyone else goes through the search, and a name that
// resolves to nobody is an error. It used to fall back to the authenticated
// user, which quietly locked the page against the very person it was meant to
// leave editable.
func (api *API) restrictionUserCloud(ctx context.Context, name string) (*User, error) {
	current, currentErr := api.GetCurrentUser(ctx)
	isLogin := api.username != "" && strings.EqualFold(name, api.username)
	if currentErr == nil && (name == "" || isLogin || current.isNamed(name)) {
		return current, nil
	}
	if name == "" {
		return nil, fmt.Errorf("unable to resolve the current user: %w", currentErr)
	}

	user, err := api.GetUserByName(ctx, name)
	if err != nil {
		if currentErr != nil {
			return nil, fmt.Errorf("unable to resolve user %q: %w", name, err)
		}
		return nil, fmt.Errorf(
			"unable to resolve user %q, and it is not the authenticated user; "+
				"refusing to restrict the page to anyone else: %w",
			name, err,
		)
	}

	return user, nil
}

func (api *API) RestrictPageUpdates(
	ctx context.Context,
	page *PageInfo,
	allowedUser string,
) error {
	userMap := map[string]any{
		"type": "known",
	}

	if api.IsCloud(ctx) {
		user, err := api.restrictionUserCloud(ctx, allowedUser)
		if err != nil {
			return fmt.Errorf("unable to restrict updates of page %s: %w", page.ID, err)
		}

		if user.AccountID == "" {
			return fmt.Errorf("resolved user %q has no accountId", allowedUser)
		}
		userMap["accountId"] = user.AccountID
	} else {
		userMap["username"] = allowedUser
	}

	// Through do rather than send: nothing in the answer is used, and Data
	// Center answers this one with 204 and no body, which send -- asked for a
	// value -- would take for a failure of a restriction already applied.
	var answer any
	_, err := api.v1.do(
		ctx, nil, http.MethodPost, []string{"content", page.ID, "restriction"}, nil,
		[]map[string]any{
			{
				"operation": "update",
				"restrictions": map[string]any{
					"user": []map[string]any{
						userMap,
					},
				},
			},
		},
		&answer,
	)
	// A 501 is not taken for an old release here: unlike the other ways of
	// saying the endpoint is missing, it falls through to the error as it is.
	if status, notServed := endpointMissing(err); notServed && status != http.StatusNotImplemented && !api.IsCloud(ctx) {
		return fmt.Errorf("confluence server/datacenter version is too old to support page edit restrictions via REST API (requires Confluence 8.8.0 or newer; status: %d)", status)
	}
	if err != nil {
		return requestError("restrict updates of page "+page.ID, err)
	}

	return nil
}

// CreateFolder creates a folder called title through the v2 API, under
// parentID when one is given. parentType names what parentID is and defaults
// to "folder"; under a folder parent, the folder goes in that parent's space
// rather than spaceID's.
func (api *API) CreateFolder(ctx context.Context, spaceID, title string, parentID *string, parentType string) (*FolderInfo, error) {
	actualSpaceID := spaceID

	if parentType == "" {
		parentType = "folder"
	}

	// If we have a folder parent, use the parent's space ID to avoid cross-space conflicts.
	if parentID != nil && parentType == "folder" {
		parentFolder, err := api.GetFolderByID(ctx, *parentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get parent folder info for space consistency: %w", err)
		}

		// A folder that is not there answers (nil, nil), not an error, and the
		// id reaching here came from a cache or from the manifest -- so it names
		// a folder that existed when it was recorded and may not now. Every
		// other caller of GetFolderByID checks this; only here did a folder
		// deleted between runs panic instead of falling back to the space.
		if parentFolder != nil && parentFolder.SpaceID != "" {
			actualSpaceID = parentFolder.SpaceID
		}
	}

	payload := map[string]interface{}{
		"spaceId": actualSpaceID,
		"title":   title,
	}

	if parentID != nil {
		payload["parentId"] = *parentID
		payload["parentType"] = parentType
	}

	folder, err := send[FolderInfo](ctx, api.v2, http.MethodPost, []string{"folders"}, payload)
	if err != nil {
		return nil, requestError(fmt.Sprintf("create folder %q in space %s", title, spaceID), err)
	}

	return &folder, nil
}

// folderCQL is the CQL search for the folders with a title in a space, which
// FindFolder and searchFolders both send. The title is a quoted CQL string,
// so a backslash or a double quote in it is escaped -- the backslash first,
// or the escape of the quote would be escaped again.
func folderCQL(spaceKey, title string) string {
	escaped := strings.ReplaceAll(title, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)

	return fmt.Sprintf(`type=folder AND title="%s" AND space="%s"`, escaped, spaceKey)
}

// searchHit is one result of a CQL search.
type searchHit struct {
	Content struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	} `json:"content"`
}

// FindFolder finds a folder with a title anywhere in a space, or anywhere
// beneath underAncestorID when that is set.
//
// "Anywhere" is meant: CQL's ancestor= matches at any depth, and only the
// first hit comes back. To find the folder directly beneath a parent, use
// FindChildFolder; for one at the space root, FindRootFolder.
func (api *API) FindFolder(ctx context.Context, spaceKey, title, underAncestorID string) (*FolderInfo, error) {
	// CQL folder search lives on /rest/api/search (not content/search).
	cql := folderCQL(spaceKey, title)
	if underAncestorID != "" {
		cql += fmt.Sprintf(` AND ancestor=%s`, underAncestorID)
	}

	query := url.Values{
		"cql":    {cql},
		"limit":  {"1"},
		"expand": {"content"},
	}

	result, err := get[listing[searchHit]](ctx, api.v1, []string{"search"}, query)
	if err != nil {
		return nil, requestError(fmt.Sprintf("search for folder %q", title), err)
	}

	if len(result.Results) == 0 || result.Results[0].Content.ID == "" {
		return nil, nil // Folder not found
	}

	item := result.Results[0].Content
	if item.Type != "folder" {
		return nil, nil
	}

	// Found a folder, now get its full details using v2 API
	return api.GetFolderByID(ctx, item.ID)
}

// FindChildFolder finds the folder with a title among the direct children of
// a page or a folder, or answers (nil, nil) when there is none. parentType is
// "page" or "folder", and picks the listing to read.
//
// Asked of the parent's own listing rather than of CQL. The search's ancestor=
// matches at any depth, so with limit=1 a folder of the same title further down
// the tree could come back in place of the one directly beneath the parent --
// and the caller, rightly refusing it, went on to create the folder, which
// Confluence refused as a duplicate. The listing is not index-lagged either, so
// a folder created moments ago is already in it.
//
// An exact title wins; failing that, one differing only in case is taken, as
// the search this replaces would have matched it.
//
// A 404 on the first page is read as "none", and the listing is skipped once
// Cloud is ruled out; see eachDirectChild.
func (api *API) FindChildFolder(ctx context.Context, parentID, parentType, title string) (*FolderInfo, error) {
	var exact, folded string
	err := api.eachDirectChild(ctx,
		parentID, parentType,
		fmt.Sprintf("look for folder %q under %s", title, parentID),
		func(id, typ, childTitle string) bool {
			if typ != "folder" {
				return false
			}
			if childTitle == title {
				exact = id
				return true
			}
			if folded == "" && strings.EqualFold(childTitle, title) {
				folded = id
			}
			return false
		},
	)
	if err != nil {
		return nil, err
	}

	switch {
	case exact != "":
		return api.GetFolderByID(ctx, exact)
	case folded != "":
		return api.GetFolderByID(ctx, folded)
	default:
		return nil, nil
	}
}

// eachDirectChild calls visit with every direct child of a page or a folder,
// in the order the v2 listing gives them, until visit answers true. parentType
// is "page" or "folder", and picks the listing to read; operation names the
// request in a transport error.
//
// A 404 on the first page reads as an empty listing, since a deployment
// without this route has no folders either; past the first page it's a real
// failure, since the listing it interrupts is already known partial.
//
// Skipped entirely once the target is identified as Server or Data Center:
// folders are Cloud-only. Not on !IsCloud, which is also false when
// identification failed -- skipping then reads a page holding folders as
// childless.
func (api *API) eachDirectChild(
	ctx context.Context,
	parentID, parentType, operation string,
	visit func(id, typ, title string) (stop bool),
) error {
	if isCloud, err := api.Cloud(ctx); err == nil && !isCloud {
		log.Debug().Msgf(
			"not listing the children of %s %s: Confluence Cloud was ruled out",
			parentType, parentID,
		)

		return nil
	}

	collection := "pages"
	if parentType == "folder" {
		collection = "folders"
	}

	type child struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	}

	return eachV2(
		ctx, api, []string{collection, parentID, "direct-children"},
		url.Values{"limit": {"100"}}, operation,
		func(c child) bool { return visit(c.ID, c.Type, c.Title) },
	)
}

// FindRootFolder finds the folder with a title at the root of a space --
// beneath neither a page nor another folder -- or answers (nil, nil).
//
// There is no listing of a space's root folders, so this goes through CQL, but
// it reads every folder of that title the search returns rather than only the
// first: with limit=1, a same-titled folder nested anywhere in the space could
// come first and hide the one at the root.
func (api *API) FindRootFolder(ctx context.Context, spaceKey, title string) (*FolderInfo, error) {
	var found *FolderInfo

	err := api.searchFolders(ctx, spaceKey, title, func(id string) (bool, error) {
		folder, err := api.GetFolderByID(ctx, id)
		if err != nil {
			return false, err
		}
		if folder == nil || folder.ParentType == "folder" || folder.ParentType == "page" {
			return false, nil
		}
		found = folder
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
}

// searchFolders runs the CQL search for the folders with a title in a space
// and hands each id to visit, following the result's next link until visit is
// done or the results run out.
func (api *API) searchFolders(ctx context.Context, spaceKey, title string, visit func(id string) (bool, error)) error {
	query := url.Values{
		"cql":    {folderCQL(spaceKey, title)},
		"limit":  {"25"},
		"expand": {"content"},
	}

	for {
		result, err := get[listing[searchHit]](ctx, api.v1, []string{"search"}, query)
		if err != nil {
			return requestError(fmt.Sprintf("search for folder %q", title), err)
		}

		for _, item := range result.Results {
			if item.Content.ID == "" || item.Content.Type != "folder" {
				continue
			}

			done, err := visit(item.Content.ID)
			if err != nil || done {
				return err
			}
		}

		if result.Links.Next == "" || len(result.Results) == 0 {
			return nil
		}

		// Cloud's next link carries a cursor, and may carry parameters of its
		// own beside it, so the link is followed as given rather than by
		// guessing which of them matter. A link that asks for what was just
		// asked would never end.
		next, err := url.Parse(result.Links.Next)
		if err != nil {
			return fmt.Errorf("unable to follow the folder search's next link: %w", err)
		}
		followed := maps.Clone(query)
		for key, values := range next.Query() {
			if len(values) > 0 {
				followed.Set(key, values[0])
			}
		}
		if followed.Encode() == query.Encode() {
			return nil
		}
		query = followed
	}
}

func (api *API) GetFolderByID(ctx context.Context, folderID string) (*FolderInfo, error) {
	folder, err := get[FolderInfo](ctx, api.v2, []string{"folders", folderID}, nil)
	if errors.Is(err, ErrNotFound) {
		return nil, nil // Folder not found
	}
	if err != nil {
		return nil, requestError("read folder "+folderID, err)
	}

	return &folder, nil
}

// GetSpaceID resolves a space key to the numeric id v2 endpoints want.
//
// Cached for the lifetime of the API value, failures included, for the same
// reason as FindHomePage: it is called for every file that has a folder in its
// ancestry and once more per manifest load, and a space key's id does not move
// while a run is going on.
func (api *API) GetSpaceID(ctx context.Context, spaceKey string) (string, error) {
	if entry, ok := api.cachedSpaceID(spaceKey); ok {
		return entry.id, entry.err
	}

	id, err := api.fetchSpaceID(ctx, spaceKey)

	if !worthCaching(err) {
		return id, err
	}

	api.spaceCacheMutex.Lock()
	if api.spaceIDCache == nil {
		api.spaceIDCache = make(map[string]spaceIDCacheEntry)
	}
	api.spaceIDCache[spaceKey] = spaceIDCacheEntry{id: id, err: err}
	api.spaceCacheMutex.Unlock()

	return id, err
}

func (api *API) cachedSpaceID(spaceKey string) (spaceIDCacheEntry, bool) {
	api.spaceCacheMutex.RLock()
	defer api.spaceCacheMutex.RUnlock()
	entry, ok := api.spaceIDCache[spaceKey]
	return entry, ok
}

func (api *API) fetchSpaceID(ctx context.Context, spaceKey string) (string, error) {
	// Try v1 first: it looks a space up by key directly, where v2 has to be
	// asked for a filtered collection.
	//
	// The response is decoded into SpaceInfo rather than a struct declared
	// here: v1 returns `id` as a JSON *number*, and a string-typed one fails
	// to decode on every call, which skips this branch and leaves the v2
	// "fallback" below the only path that runs. SpaceInfo already types the
	// field correctly and is exercised against real Confluence by
	// FindHomePage, so there is no reason for a second, divergent shape.
	//
	// v2 keeps its own string-typed struct: the two APIs genuinely disagree
	// about this field, which is what made the mismatch easy to miss.
	var v1Err error

	// Through the gateway v1 is not asked at all: a scoped token is refused
	// there, and v2 answers either kind of token.
	if !api.gateway {
		var v1Result SpaceInfo
		v1Result, v1Err = get[SpaceInfo](ctx, api.v1, []string{"space", spaceKey}, nil)
		if v1Err == nil && v1Result.ID != 0 {
			return strconv.Itoa(v1Result.ID), nil
		}
	}

	// Fallback to v2 API with query parameter
	v2Result, err := get[listing[struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}]](ctx, api.v2, []string{"spaces"}, url.Values{"keys": {spaceKey}})
	err = requestError("look up the id of space "+spaceKey, err)

	// When v2 cannot answer either, report why v1 refused, as FindHomePage
	// does. Server and Data Center have no /api/v2, so there v2's 404 says
	// nothing about the space: it replaced a 401 with a misleading 404, and,
	// being a 404, got a v1 outage cached for the rest of the run. v1's answer
	// is the one that decides, so v2's is kept for the message only.
	if err != nil {
		return "", api.spaceFallbackError("read space "+spaceKey, v1Err, err)
	}

	api.learnSiteBase(v2Result.Links.Base)

	// v2 answered, and knows of no such space: a conclusion, so wrapped in the
	// sentinel that lets GetSpaceID remember it.
	if len(v2Result.Results) == 0 {
		return "", fmt.Errorf("space with key %s not found: %w", spaceKey, ErrNotFound)
	}

	return v2Result.Results[0].ID, nil
}

// CreatePageWithFolderParent creates a page with a folder as parent using REST API v2.
//
// Folders exist only in v2, so this is the v2 create CreatePage makes through
// the gateway, with the folder named as the parent.
func (api *API) CreatePageWithFolderParent(
	ctx context.Context,
	space string,
	pageType string,
	folderID string,
	title string,
	body string,
) (*PageInfo, error) {
	page, err := api.createPageV2(ctx, space, pageType, folderID, "folder", title, body)
	if err != nil {
		return nil, err
	}

	api.rememberCreatedPage(space, pageType, title, page)

	return page, nil
}

// GetChildPages returns a page's children in the order Confluence shows them.
//
// The order of this response is the order of the tree in the UI, which is what
// makes it usable for working out which pages are already where they should be.
// Position is not requested explicitly: it exists as an extension field but is
// absent whenever a branch has never been ordered by hand, and the sequence is
// the part that matters here.
func (api *API) GetChildPages(ctx context.Context, parentID string) ([]PageInfo, error) {
	return listV1[PageInfo](
		ctx, api, []string{"content", parentID, "child", "page"},
		nil, 100, "list child pages of "+parentID, false, nil,
	)
}

// HasChildFolders reports whether a folder sits directly beneath a page.
//
// Folders are v2 entities and never appear in content/{id}/child/page, which
// lists content of type page and nothing else. A page whose only children are
// folders therefore looks childless there -- while trashing it takes the
// folders, and every page inside them, along with it.
//
// A 404 is read as "none", any other failure is reported, and the request is
// skipped once Cloud is ruled out; see eachDirectChild.
//
// Stops at the first folder it sees. The answer is a yes or a no, and the rest
// of the listing cannot change it.
func (api *API) HasChildFolders(ctx context.Context, parentID string) (bool, error) {
	found := false
	err := api.eachDirectChild(ctx,
		parentID, "page", "list direct children of "+parentID,
		func(_, typ, _ string) bool {
			found = typ == "folder"
			return found
		},
	)
	if err != nil {
		return false, err
	}

	return found, nil
}

// DeletePage moves a page to the space's trash.
//
// Trash, not oblivion: Confluence keeps a deleted page recoverable until
// somebody purges it, and purging is a second call this deliberately does not
// make. A tool that removes pages because a file left a repository should leave
// the last word to a person.
func (api *API) DeletePage(ctx context.Context, contentID string) error {
	_, err := api.v1.do(ctx, nil, http.MethodDelete, []string{"content", contentID}, nil, nil, &struct{}{})
	// 204 is the documented answer, though some versions send 200, and a page
	// already gone is the outcome that was wanted anyway.
	if err != nil && !errors.Is(err, ErrNotFound) {
		return requestError("delete content "+contentID, err)
	}

	return nil
}

// ArchivePage asks for a page to be archived, which is gentler than the trash:
// it leaves the page findable and restorable without an administrator.
//
// The identifier goes in as a number. Confluence documents the body as
// {"pages":[{"id":<number>}]} and rejects a quoted one with 400, which is easy
// to miss because every other endpoint here takes an id as a string.
//
// The answer is 202: the request has been accepted and the archiving happens
// afterwards, reported through the long-running task the response names. mark
// does not wait for it. A page that fails to archive after acceptance is not
// noticed here, which is worth knowing before relying on this in a place where
// it matters.
//
// Cloud only. Server and Data Center have no archive and say so, which is
// reported rather than swallowed so that a run asking to archive is not
// silently doing nothing.
func (api *API) ArchivePage(ctx context.Context, contentID string) error {
	id, err := strconv.ParseInt(contentID, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot archive content %q: not a page id: %w", contentID, err)
	}

	payload := map[string]any{
		"pages": []map[string]any{{"id": id}},
	}

	_, err = api.v1.do(ctx, nil, http.MethodPost, []string{"content", "archive"}, nil, payload, &struct{}{})
	if err != nil {
		return requestError("archive content "+contentID, err)
	}

	return nil
}

// MoveContentAfter places a page immediately after one of its siblings.
//
// Unlike the append form, the target here is a sibling rather than the new
// parent. Atlassian warn against using it when the target is a top-level page,
// where it can move content to the root of the space; callers are expected not
// to.
func (api *API) MoveContentAfter(ctx context.Context, contentID, siblingID string) error {
	return api.orderContent(ctx, contentID, "after", siblingID)
}

// MoveContentBefore places a page immediately before one of its siblings. The
// same caution about top-level targets applies as for MoveContentAfter.
func (api *API) MoveContentBefore(ctx context.Context, contentID, siblingID string) error {
	return api.orderContent(ctx, contentID, "before", siblingID)
}

// MoveContentAppend relocates any content (page, folder, etc.) under targetID.
//
// The v1 move endpoint is tried first, and on Cloud that is the whole of it.
// A Server or Data Center instance answers it 404, and the page is then moved
// with /pages/movepage.action, the action Confluence's own Move dialog posts
// to. Where that is not usable either, the page is moved the way those
// releases document a move: an update carrying the new ancestor.
//
// Whether DC lacks the endpoint outright is still unverified from here; the
// fallbacks do not depend on knowing. A 404 that is really a missing page
// reaches them too, and the update then fails on the same page for the same
// reason, saying so in terms of the page rather than the endpoint.
func (api *API) MoveContentAppend(ctx context.Context, contentID, targetID string) error {
	err := api.moveContent(ctx, contentID, "append", targetID)
	if !errors.Is(err, errNoMoveEndpoint) {
		return err
	}

	moved, err := api.moveByAction(ctx, contentID, "append", targetID)
	if moved {
		return nil
	}

	log.Debug().Err(err).Msgf(
		"this Confluence moved neither through the content move endpoint nor "+
			"movepage.action; reparenting %s under %s with an update instead",
		api.describeContent(contentID), targetID,
	)

	return api.reparentContent(ctx, contentID, targetID)
}

// orderContent places content before or after one of its siblings.
//
// Outside Cloud the only fallback is movepage.action: position among siblings
// is not part of what an update can carry.
func (api *API) orderContent(ctx context.Context, contentID, position, siblingID string) error {
	err := api.moveContent(ctx, contentID, position, siblingID)
	if !errors.Is(err, errNoMoveEndpoint) {
		return err
	}

	moved, actionErr := api.moveByAction(ctx, contentID, position, siblingID)
	if moved {
		return nil
	}

	return fmt.Errorf(
		"unable to place %s %s %s: ordering pages needs the content move "+
			"endpoint or /pages/movepage.action, and neither worked; drop the "+
			"Order header for these pages, or arrange them in Confluence by "+
			"hand: %w",
		api.describeContent(contentID), position, siblingID,
		errors.Join(err, actionErr),
	)
}

// moveSettleAttempts bounds how often a claimed movepage.action success is
// read back before it is taken as having done nothing.
const moveSettleAttempts = 8

// actionPositions maps the REST move positions onto movepage.action's.
var actionPositions = map[string]string{
	"append": "append",
	"before": "above",
	"after":  "below",
}

// moveByAction moves content with /pages/movepage.action and reports whether
// the content ended up where it was asked to go.
//
// The action is what the Move dialog of Server and Data Center posts to, not a
// documented API, so its answer is not trusted either way: success is judged by
// reading the tree back afterwards.
func (api *API) moveByAction(ctx context.Context, contentID, position, targetID string) (bool, error) {
	if missing := api.moveActionMissing.Load(); missing != nil {
		return false, *missing
	}

	before, err := api.GetPageByID(ctx, contentID)
	if err != nil {
		return false, fmt.Errorf(
			"unable to read %s to move it: %w", api.describeContent(contentID), err,
		)
	}

	// Through get, so that an empty answer is an error: it would leave the
	// target with no space and no ancestors, so an ordering would report the
	// target as at the root of its space, and an append would ask the action
	// -- a write -- to move the page under a title and space of "".
	target, err := get[struct {
		Title string `json:"title"`
		Space struct {
			Key string `json:"key"`
		} `json:"space"`
		Ancestors []struct {
			ID string `json:"id"`
		} `json:"ancestors"`
	}](ctx, api.v1, []string{"content", targetID}, url.Values{"expand": {"space,ancestors"}})
	if err != nil {
		return false, requestError("read move target "+targetID, err)
	}
	if target.Space.Key == "" {
		return false, fmt.Errorf("unable to read move target %s: its answer names no space", targetID)
	}

	// The parent the content has to end up under; for before and after that
	// is the sibling's.
	parentID := targetID
	if position != "append" {
		if len(target.Ancestors) == 0 {
			return false, fmt.Errorf(
				"unable to place content next to %s: it is at the root of its space", targetID,
			)
		}
		parentID = target.Ancestors[len(target.Ancestors)-1].ID
	}

	if position == "append" && immediateParent(before) == parentID {
		return true, nil
	}

	query := url.Values{
		"pageId":      {contentID},
		"position":    {actionPositions[position]},
		"targetId":    {targetID},
		"targetTitle": {target.Title},
		"spaceKey":    {target.Space.Key},
	}
	// Asks Seraph to authenticate a non-REST request from the basic auth header.
	if api.site.username != "" {
		query.Set("os_authType", "basic")
	}

	var answer map[string]any
	response, err := api.site.do(
		ctx,
		http.Header{"X-Atlassian-Token": {"no-check"}, "Accept": {"application/json"}},
		http.MethodPost, []string{"pages", "movepage.action"}, query, nil,
		&whateverContentType{out: &answer},
	)
	// A response cut short by the run's context is no answer from the action,
	// however much of it arrived: judged here, a cancelled move would be taken
	// for a closed action and remembered for the rest of the API's life.
	if response == nil || IsCancellation(err) {
		return false, requestError(fmt.Sprintf("move content %s %s %s", contentID, position, targetID), err)
	}

	// Beyond the statuses that say the action is not there, a 401 is about
	// the credentials, not the page, so it too holds for the rest of the run.
	status, notServed := endpointMissing(err)
	if notServed || status == http.StatusUnauthorized {
		missing := fmt.Errorf("%w (status: %d)", errNoMoveAction, status)
		api.moveActionMissing.Store(&missing)

		return false, missing
	}
	if status != 0 {
		return false, err
	}

	// A body that does not decode is no answer from the action. A login page
	// means the action is closed to these credentials, for this page and every
	// other, so it is not asked again this run. HTML and nothing else to go on
	// is not that: the action may redirect to the page it moved, and a proxy's
	// page for this one request says nothing about the next page.
	notJSON := err != nil
	var login *loginPageError
	if errors.As(err, &login) && login.strong {
		missing := fmt.Errorf("%w: it answered with a login page: %w", errNoMoveAction, err)
		api.moveActionMissing.Store(&missing)

		return false, missing
	}

	// Data Center can answer before a reparent shows in the REST view, so an
	// answer that is not a refusal is given a moment to show; a refusal is not.
	attempts := 1
	if notJSON || actionErrors(answer) == "" {
		attempts = moveSettleAttempts
	}

	for attempt := range attempts {
		if attempt > 0 {
			if err := api.wait(ctx, api.moveSettleDelay); err != nil {
				return false, fmt.Errorf(
					"stopped waiting for movepage.action to move %s: %w",
					api.describeContent(contentID), err,
				)
			}
		}

		placed, verifyErr := api.placedAt(ctx, contentID, position, targetID, parentID, before)
		if verifyErr != nil {
			return false, verifyErr
		}
		if placed {
			return true, nil
		}
	}

	if notJSON {
		return false, fmt.Errorf(
			"movepage.action answered with something other than JSON (status: %d) "+
				"and left %s where it was",
			response.StatusCode, api.describeContent(contentID),
		)
	}

	return false, fmt.Errorf(
		"movepage.action left %s where it was%s",
		api.describeContent(contentID), actionErrors(answer),
	)
}

// placedAt reads the tree back after movepage.action and reports whether
// contentID now sits where position and targetID put it.
func (api *API) placedAt(
	ctx context.Context,
	contentID, position, targetID, parentID string, before *PageInfo,
) (bool, error) {
	after, err := api.GetPageByID(ctx, contentID)
	if err != nil {
		return false, fmt.Errorf(
			"unable to read %s after moving it: %w", api.describeContent(contentID), err,
		)
	}
	if immediateParent(after) != parentID {
		return false, nil
	}

	// Record a version the move wrote as mark's own, so that --no-overwrite
	// does not take it for an edit.
	if after.Version.Number != before.Version.Number {
		api.updateCachedPageVersion(contentID, after.Version.Number)
		api.noteReparent(contentID, before.Version.Number, after.Version.Number)
	}

	if position == "append" {
		return true, nil
	}

	children, err := api.GetChildPages(ctx, parentID)
	if err != nil {
		return false, fmt.Errorf("unable to list children of %s after a move: %w", parentID, err)
	}

	at := slices.IndexFunc(children, func(p PageInfo) bool { return p.ID == contentID })
	sibling := slices.IndexFunc(children, func(p PageInfo) bool { return p.ID == targetID })
	if at < 0 || sibling < 0 {
		return false, nil
	}

	if position == "before" {
		return at == sibling-1, nil
	}

	return at == sibling+1, nil
}

func immediateParent(page *PageInfo) string {
	if page == nil || len(page.Ancestors) == 0 {
		return ""
	}

	return page.Ancestors[len(page.Ancestors)-1].ID
}

// actionErrors formats whatever errors movepage.action put in its answer.
//
// The action is XWork's, which reports a refusal under actionErrors, under
// fieldErrors keyed by field, or in validationErrors or errorMessage, as a
// string, a list, or a map of either. A refusal it could not read would be
// taken for success, waited on, and then reported without its reason.
func actionErrors(answer map[string]any) string {
	var messages []string
	for _, key := range []string{"actionErrors", "fieldErrors", "validationErrors", "errorMessage"} {
		messages = appendMessages(messages, "", answer[key])
	}

	if len(messages) == 0 {
		return ""
	}

	return ": " + strings.Join(messages, "; ")
}

// appendMessages collects the text of an error value, naming the field a map
// keyed it by.
func appendMessages(messages []string, field string, value any) []string {
	prefix := ""
	if field != "" {
		prefix = field + ": "
	}

	switch value := value.(type) {
	case string:
		if value != "" {
			messages = append(messages, prefix+value)
		}
	case []any:
		for _, item := range value {
			messages = appendMessages(messages, field, item)
		}
	case map[string]any:
		for _, name := range slices.Sorted(maps.Keys(value)) {
			messages = appendMessages(messages, name, value[name])
		}
	case nil:
	default:
		messages = append(messages, prefix+fmt.Sprint(value))
	}

	return messages
}

// reparentContent moves content under parentID through the update API.
//
// Confluence documents ancestors on an update as the way a page is moved, and
// on Server and Data Center it is the only way: there is no move endpoint to
// call. The body is read and written back unchanged rather than left out of
// the payload, because what an update without a body does to the one already
// there is the server's business, and a move is not allowed to cost a page its
// content on the reading where it blanks it.
func (api *API) reparentContent(ctx context.Context, contentID, parentID string) error {
	page, placed, err := api.readForReparent(ctx, contentID, parentID)
	if err != nil || placed {
		return err
	}

	err = api.reparentContentV1(ctx, page, parentID, page.Version.Number+1)
	if errors.Is(err, ErrConflict) {
		// Something wrote to the page between the read and the write. The
		// payload carries the body and version message of that read, so
		// sending it again at the next version -- which is what
		// retryConflictingUpdate does for a publish, where mark's body is the
		// point -- would put back what the page said before the edit and undo
		// it. Here the body is only a passenger, so the whole page is read
		// again and the move rebuilt from that. Nothing is overwritten this
		// way, which is also why KeepConcurrentEdits has nothing to refuse.
		attempted := page.Version.Number + 1

		if err := api.wait(ctx, api.conflictRetryDelay); err != nil {
			return fmt.Errorf(
				"move of page %q (%s) as version %d conflicted (%w), and was not retried: %w",
				page.Title, page.ID, attempted, ErrConflict, err,
			)
		}

		page, placed, err = api.readForReparent(ctx, contentID, parentID)
		if err != nil || placed {
			return err
		}

		log.Debug().Msgf(
			"move of page %q (%s) as version %d conflicted; retrying as version %d",
			page.Title, page.ID, attempted, page.Version.Number+1,
		)

		err = api.reparentContentV1(ctx, page, parentID, page.Version.Number+1)
		if errors.Is(err, ErrConflict) {
			err = fmt.Errorf(
				"version %d was refused as a conflict, and version %d again "+
					"after reading the page afresh; something else is writing to "+
					"the page at the same time, so run mark again once it has "+
					"stopped: %w",
				attempted, page.Version.Number+1, err,
			)
		}
	}
	if err != nil {
		return fmt.Errorf(
			"unable to move page %q (%s) under %s: %w", page.Title, page.ID, parentID, err,
		)
	}

	api.updateCachedPageVersion(page.ID, page.Version.Number+1)
	api.noteReparent(page.ID, page.Version.Number, page.Version.Number+1)

	return nil
}

// reparentedVersions is the version a page was found at before a move by
// update, and the version that move wrote.
type reparentedVersions struct {
	from, to int64
}

// noteReparent records that a move by update took a page from version from
// to version to. A page moved twice in a run keeps the version it had before
// the first move, since everything after that was written by mark.
func (api *API) noteReparent(pageID string, from, to int64) {
	api.reparentedMutex.Lock()
	defer api.reparentedMutex.Unlock()

	if api.reparented == nil {
		api.reparented = map[string]reparentedVersions{}
	}
	if earlier, ok := api.reparented[pageID]; ok && earlier.to == from {
		from = earlier.from
	}
	api.reparented[pageID] = reparentedVersions{from: from, to: to}
}

// ReparentedFrom reports the version a page was at before this API moved it,
// when the page is still at the version the move wrote.
//
// On Server and Data Center a move is an update, and an update is a version.
// That version is mark's own, but it is indistinguishable by number from an
// edit made in Confluence, which is what --no-overwrite compares numbers to
// find. A caller holding a page at current asks here which version to compare
// instead. A page that has moved on since the move is answered false: what
// came after it was not mark.
//
// The move endpoint Cloud uses writes no version, so only a move by update is
// recorded; a page moved that way has nothing to discount.
func (api *API) ReparentedFrom(pageID string, current int64) (int64, bool) {
	api.reparentedMutex.Lock()
	defer api.reparentedMutex.Unlock()

	moved, ok := api.reparented[pageID]
	if !ok || moved.to != current {
		return 0, false
	}

	return moved.from, true
}

// readForReparent reads everything reparentContentV1 writes back, and reports
// placed when the page already sits under parentID: Confluence accepts an
// update that names the parent a page already has, but it costs the page a
// version for nothing.
func (api *API) readForReparent(ctx context.Context, contentID, parentID string) (*PageInfo, bool, error) {
	page, err := api.GetPageByIDExpanded(ctx, contentID, "ancestors,version,body.storage")
	if err != nil {
		return nil, false, fmt.Errorf(
			"unable to read %s to move it under %s: %w",
			api.describeContent(contentID), parentID, err,
		)
	}
	if page == nil {
		return nil, false, fmt.Errorf(
			"unable to move content %s under %s: no such content", contentID, parentID,
		)
	}

	n := len(page.Ancestors)

	return page, n > 0 && page.Ancestors[n-1].ID == parentID, nil
}

// reparentContentV1 is the request behind reparentContent; nextVersion is the
// version the move creates.
func (api *API) reparentContentV1(ctx context.Context, page *PageInfo, parentID string, nextVersion int64) error {
	pageType := page.Type
	if pageType == "" {
		pageType = "page"
	}

	payload := map[string]any{
		"id":    page.ID,
		"type":  pageType,
		"title": page.Title,
		"version": map[string]any{
			"number":    nextVersion,
			"minorEdit": true,
			// The message is carried over rather than replaced by something
			// about the move. mark keeps the content fingerprint --changes-only
			// reads inside it, and the caller refreshes the page from the
			// server the moment this returns: a move that wrote its own message
			// would tell the rest of that run, and every run after it, that the
			// content had changed when only the page's place in the tree did.
			// The move is a version of its own either way, so the history still
			// shows it.
			"message": page.Version.Message,
		},
		"ancestors": []map[string]any{
			{"id": parentID},
		},
	}

	if value := page.Body.Storage.Value; value != "" {
		payload["body"] = map[string]any{
			"storage": map[string]any{
				"value":          value,
				"representation": "storage",
			},
		}
	}

	_, err := api.v1.do(
		ctx, nil, http.MethodPut, []string{"content", page.ID}, nil, payload, &struct{}{},
	)

	return api.confirmVersion(
		ctx, page, nextVersion,
		requestError(fmt.Sprintf("move page %q (%s)", page.Title, page.ID), err),
	)
}

func (api *API) moveContent(ctx context.Context, contentID, position, targetID string) error {
	if missing := api.moveEndpointMissing.Load(); missing != nil {
		return *missing
	}

	_, err := api.v1.do(
		ctx, nil, http.MethodPut, []string{"content", contentID, "move", position, targetID},
		nil, map[string]any{}, &struct{}{},
	)

	if status, notServed := endpointMissing(err); notServed {
		// content/{id}/move is a Cloud endpoint, reached whenever a page's
		// ancestry stops matching its headers or whenever pages are ordered.
		// Outside Cloud it is not there, and the answer is reported as such so
		// that the callers above can decide what to do about it: a reparent
		// has another way round, an ordering has none.
		//
		// A 404 that really is a missing page arrives looking exactly the
		// same, which is why the status stays in the message and why nothing
		// here concludes anything about the content itself.
		if isCloud, err := api.Cloud(ctx); !isCloud {
			// Cloud answers false, and remembers nothing, when its probe
			// was cancelled. That is no reason to conclude the endpoint is
			// missing, and remembered, it would send every later Cloud move on
			// this API down the Data Center fallbacks.
			if IsCancellation(err) {
				return fmt.Errorf(
					"unable to move content %s %s %s: %w", contentID, position, targetID, err,
				)
			}

			missing := fmt.Errorf("%w (status: %d)", errNoMoveEndpoint, status)
			api.moveEndpointMissing.Store(&missing)

			return missing
		}
	}
	if err != nil {
		return requestError(fmt.Sprintf("move content %s %s %s", contentID, position, targetID), err)
	}

	return nil
}

// describeContent names a page for an error message, falling back to its id.
//
// The cache is consulted rather than the API: this is only ever reached after
// something has already failed, and a second request made to phrase a message
// more nicely is a second thing that can go wrong.
func (api *API) describeContent(contentID string) string {
	api.lazyInit()
	api.pageCacheMutex.RLock()
	defer api.pageCacheMutex.RUnlock()

	if page, ok := api.pageCacheByID[contentID]; ok && page != nil && page.Title != "" {
		return fmt.Sprintf("page %q (%s)", page.Title, contentID)
	}

	return "content " + contentID
}

// errNoMoveAction reports that /pages/movepage.action is missing, refuses the
// credentials, or answers them with something other than JSON.
var errNoMoveAction = errors.New(
	"this Confluence did not serve /pages/movepage.action to mark",
)

// errNoMoveEndpoint reports that a non-Cloud Confluence answered
// content/{id}/move with 404, 405 or 501, which sends both callers on to
// movepage.action.
var errNoMoveEndpoint = errors.New(
	"this Confluence did not serve the content move endpoint, which is Cloud-only",
)
