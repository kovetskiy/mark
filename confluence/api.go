package confluence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kovetskiy/gopencils"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type User struct {
	AccountID string `json:"accountId,omitempty"`
	UserKey   string `json:"userKey,omitempty"`
	Username  string `json:"username,omitempty"`
}

type API struct {
	rest *gopencils.Resource
	// v2 API for newer endpoints like folders
	restV2  *gopencils.Resource
	BaseURL string

	// bearerToken is the Personal Access Token used when no username was
	// given. It is kept here instead of being installed on rest/restV2 because
	// those two would then hand it, and their whole header map, to every
	// resource derived from them; see resource().
	bearerToken string

	isCloudFlag bool
	isCloudOnce sync.Once

	pageCache      map[string]*PageInfo
	pageCacheByID  map[string]*PageInfo
	pageCacheMutex sync.RWMutex

	userCache      map[string]userCacheEntry
	userCacheMutex sync.RWMutex

	// Both maps are keyed by space key and guarded by the one mutex: they
	// answer the same question about the same thing and are always populated
	// from the same place in a run.
	homePageCache   map[string]homePageCacheEntry
	spaceIDCache    map[string]spaceIDCacheEntry
	spaceCacheMutex sync.RWMutex
}

// userCacheEntry records the outcome of a user lookup, including a failed one:
// a document that mentions an unknown name would otherwise re-query for every
// occurrence, which is the case the cache is least able to afford.
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

func pageCacheKey(space, title, pageType string) string {
	return space + "\x00" + title + "\x00" + pageType
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

	Version struct {
		Number  int64  `json:"number"`
		Message string `json:"message"`
	} `json:"version"`

	Ancestors []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"ancestors"`

	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`

	Links struct {
		Full string `json:"webui"`
		Base string `json:"-"` // Not from JSON; populated from response _links.base
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
type form struct {
	buffer io.Reader
	writer *multipart.Writer
}

type tracer struct {
	prefix string
}

func (tracer *tracer) Printf(format string, args ...any) {
	log.Trace().Msgf(tracer.prefix+" "+format, args...)
}

func NewAPI(baseURL string, username string, password string, insecureSkipVerify bool) *API {
	var auth *gopencils.BasicAuth
	if username != "" {
		auth = &gopencils.BasicAuth{
			Username: username,
			Password: password,
		}
	}

	// Normalize baseURL once before building all derived endpoints.
	baseURL = strings.TrimSuffix(baseURL, "/")

	httpClient := newHTTPClient(insecureSkipVerify)

	// gopencils is given 0 retries: its own retry loop only runs when the very
	// first Client.Do returns a transport error, so a 429 or 503 -- which come
	// back with a nil error -- was never retried at all. retryTransport in the
	// client handles both cases, and is the single place retries happen.
	rest := gopencils.Api(baseURL+"/rest/api", auth, httpClient, 0)
	restV2 := gopencils.Api(baseURL+"/api/v2", auth, httpClient, 0) // v2 API for folders and new features

	if zerolog.GlobalLevel() == zerolog.TraceLevel {
		rest.Logger = &tracer{"rest:"}
		restV2.Logger = &tracer{"rest-v2:"}
	}

	api := &API{
		rest:          rest,
		restV2:        restV2,
		BaseURL:       baseURL,
		pageCache:     make(map[string]*PageInfo),
		pageCacheByID: make(map[string]*PageInfo),
	}

	// A Personal Access Token arrives as the password with no username. It is
	// recorded rather than installed on rest/restV2 so that resource() can put
	// it on a header map belonging to one request.
	if username == "" {
		api.bearerToken = password
	}

	return api
}

// v1 returns a request-scoped resource rooted at /rest/api, and v2 the same for
// /api/v2. Every call in this package starts from one of them.
//
// The indirection is what keeps one request's headers out of the next one's.
// gopencils hands the same http.Header map to a resource and to everything
// Res() derives from it, and after each response it copies that response's
// headers into the map with Add. A header map installed on the two roots
// therefore lived for the whole run: it grew one value per header per request,
// and since gopencils sends the *first* value ever recorded for a key, a single
// odd Content-Type -- an SSO login page, a proxy interstitial, anything
// answering below 400 -- pinned that Content-Type on every later PUT and POST
// body for the rest of the run. CreateAttachment used to escape this only by
// rebinding Headers to a fresh map by hand.
func (api *API) v1() *gopencils.Resource {
	return api.resource(api.rest)
}

func (api *API) v2() *gopencils.Resource {
	return api.resource(api.restV2)
}

func (api *API) resource(root *gopencils.Resource) *gopencils.Resource {
	headers := http.Header{}
	if api.bearerToken != "" {
		headers.Set("Authorization", "Bearer "+api.bearerToken)
	}

	return &gopencils.Resource{
		Api:     root.Api,
		Url:     root.Url,
		Headers: headers,
		Logger:  root.Logger,
	}
}

func (api *API) FindRootPage(space string) (*PageInfo, error) {
	page, err := api.FindPage(space, ``, "page")
	if err != nil {
		return nil, fmt.Errorf("can't obtain first page from space %q: %w", space, err)
	}

	if page == nil {
		return nil, errors.New("no such space")
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

// FindHomePage returns a space's home page.
//
// Results are cached for the lifetime of the API value, failures included. The
// call is made for every non-blogpost document, again for any page that turns
// out to have no ancestors, and once more when the manifest loads -- and it
// costs two requests rather than one on a scoped token, because the v1 refusal
// is what sends it to v2. A space's home page cannot change mid-run, so asking
// twice can only ever get the same answer.
func (api *API) FindHomePage(space string) (*PageInfo, error) {
	if entry, ok := api.cachedHomePage(space); ok {
		return clonePageInfo(entry.page), entry.err
	}

	page, err := api.fetchHomePage(space)

	api.spaceCacheMutex.Lock()
	if api.homePageCache == nil {
		api.homePageCache = make(map[string]homePageCacheEntry)
	}
	api.homePageCache[space] = homePageCacheEntry{page: clonePageInfo(page), err: err}
	api.spaceCacheMutex.Unlock()

	return page, err
}

func (api *API) cachedHomePage(space string) (homePageCacheEntry, bool) {
	api.spaceCacheMutex.RLock()
	defer api.spaceCacheMutex.RUnlock()
	entry, ok := api.homePageCache[space]
	return entry, ok
}

func (api *API) fetchHomePage(space string) (*PageInfo, error) {
	payload := map[string]string{
		"expand": "homepage",
	}

	v1Request, v1Err := api.v1().Res(
		"space/"+space, &SpaceInfo{},
	).Get(payload)
	if v1Err == nil && v1Request.Raw.StatusCode == http.StatusOK {
		homepage := &v1Request.Response.(*SpaceInfo).Homepage

		// A 200 does not guarantee a homepage came with it. Returning the zero
		// PageInfo handed the caller an empty id, which then went out as a
		// content id -- a page published under nothing at all. v2 already tells
		// "space has no homepage" apart from "no such space"; v1 says the same
		// thing here rather than falling through to a v2 that does not exist on
		// Server or Data Center.
		if homepage.ID == "" {
			return nil, fmt.Errorf("space %s has no home page", space)
		}

		return homepage, nil
	}

	// Any non-OK v1 answer falls through to v2, mirroring GetSpaceID. The case
	// that motivated the fallback is a scoped API token going through the
	// api.atlassian.com gateway: those tokens lack the classic scopes, so the
	// v1 space endpoint answers 404 and every run aborted here. The fallback is
	// deliberately not narrowed to 404: a token with partial scopes can also
	// draw 401/403 from v1 while v2 still answers.
	v2Result := struct {
		Results []struct {
			ID         string `json:"id"`
			HomepageID string `json:"homepageId"`
		} `json:"results"`
	}{}

	v2Request, v2Err := api.v2().Res(
		"spaces", &v2Result,
	).Get(map[string]string{"keys": space})
	if v2Err == nil && v2Request.Raw.StatusCode != http.StatusOK {
		v2Err = newErrorStatusNotOK(v2Request)
	}

	// When v2 cannot answer either, report why v1 refused rather than why v2
	// did. Server/DC has no /api/v2 at all, so every fallback there ends in a
	// 404 from a path that was never going to exist, and surfacing that instead
	// of v1's answer turns a clear "401 (Unauthorized)" -- the error a Server
	// user with bad credentials should see -- into a misleading "404".
	if v2Err != nil {
		if v1Err == nil {
			v1Err = newErrorStatusNotOK(v1Request)
		}
		return nil, fmt.Errorf("v1 API: %w (v2 fallback also failed: %w)", v1Err, v2Err)
	}

	if len(v2Result.Results) == 0 {
		return nil, fmt.Errorf("space with key %s not found", space)
	}

	// A space that exists but has no homepage is a different failure from a
	// space that does not exist, and reporting it as "not found" sends people
	// looking for a typo in a space key that is perfectly correct.
	if v2Result.Results[0].HomepageID == "" {
		return nil, fmt.Errorf("space %s has no home page", space)
	}

	return api.GetPageByID(v2Result.Results[0].HomepageID)
}

func (api *API) FindPage(
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

	result := struct {
		Results []PageInfo `json:"results"`
		Links   struct {
			Base string `json:"base"`
		} `json:"_links"`
	}{}

	payload := map[string]string{
		"spaceKey": space,
		"expand":   "ancestors,version",
		"type":     pageType,
	}

	if title != "" {
		payload["title"] = title
	}

	request, err := api.v1().Res(
		"content/", &result,
	).Get(payload)
	if err != nil {
		return nil, err
	}

	// allow 404 because it's fine if page is not found,
	// the function will return nil, nil
	if request.Raw.StatusCode != http.StatusNotFound && request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	api.pageCacheMutex.Lock()
	defer api.pageCacheMutex.Unlock()

	// Double-checked locking: check if the cache was populated by another goroutine
	// while the network request was in-flight.
	if cachedPage, ok := api.pageCache[key]; ok {
		return clonePageInfo(cachedPage), nil
	}

	if len(result.Results) == 0 {
		api.setCacheEntry(key, nil)
		return nil, nil
	}

	page := result.Results[0]
	// Populate the base URL from the response _links.base or fallback to BaseURL
	if result.Links.Base != "" {
		page.Links.Base = result.Links.Base
	} else if page.Links.Base == "" {
		page.Links.Base = api.BaseURL
	}

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

func (api *API) CreateAttachment(
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

	var result struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}

	resource := api.v1().Res(
		"content/"+pageID+"/child/attachment", &result,
	)

	resource.Payload = form.buffer
	// The multipart Content-Type has to be the only one on the request. It used
	// to be set on a hand-made replacement header map because the map reached
	// here carrying every header of every earlier response; resource() now
	// gives each request a map of its own, so setting it is enough.
	resource.SetHeader("Content-Type", form.writer.FormDataContentType())
	resource.SetHeader("X-Atlassian-Token", "no-check")

	request, err := resource.Post()
	if err != nil {
		return info, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return info, newErrorStatusNotOK(request)
	}

	if len(result.Results) == 0 {
		return info, errors.New(
			"the Confluence REST API for creating attachments returned " +
				"0 json objects, expected at least 1",
		)
	}

	for i, info := range result.Results {
		if info.Links.Context == "" {
			info.Links.Context = result.Links.Context
		}

		result.Results[i] = info
	}

	info = result.Results[0]

	return info, nil
}

// UpdateAttachment uploads a new version of the same attachment if the
// checksums differs from the previous one.
// It also handles a case where Confluence returns sort of "short" variant of
// the response instead of an extended one.
func (api *API) UpdateAttachment(
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

	var extendedResponse struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}

	var result json.RawMessage

	resource := api.v1().Res(
		"content/"+pageID+"/child/attachment/"+attachID+"/data", &result,
	)

	resource.Payload = form.buffer
	// The multipart Content-Type has to be the only one on the request. It used
	// to be set on a hand-made replacement header map because the map reached
	// here carrying every header of every earlier response; resource() now
	// gives each request a map of its own, so setting it is enough.
	resource.SetHeader("Content-Type", form.writer.FormDataContentType())
	resource.SetHeader("X-Atlassian-Token", "no-check")

	request, err := resource.Post()
	if err != nil {
		return info, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return info, newErrorStatusNotOK(request)
	}

	err = json.Unmarshal(result, &extendedResponse)
	if err != nil {
		return info, fmt.Errorf("unable to unmarshal JSON response as full response format (JSON=%s): %w", string(result), err)
	}

	if len(extendedResponse.Results) > 0 {
		for i, info := range extendedResponse.Results {
			if info.Links.Context == "" {
				info.Links.Context = extendedResponse.Links.Context
			}

			extendedResponse.Results[i] = info
		}

		info = extendedResponse.Results[0]

		return info, nil
	}

	var shortResponse AttachmentInfo
	err = json.Unmarshal(result, &shortResponse)
	if err != nil {
		return info, fmt.Errorf("unable to unmarshal JSON response as short response format (JSON=%s): %w", string(result), err)
	}

	return shortResponse, nil
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

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("unable to close form-writer: %w", err)
	}

	return &form{
		buffer: payload,
		writer: writer,
	}, nil
}

func (api *API) GetAttachments(pageID string) ([]AttachmentInfo, error) {
	type page struct {
		Links struct {
			Context string `json:"context"`
			Next    string `json:"next"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}

	const pageSize = 100
	var all []AttachmentInfo
	start := 0

	for {
		var result page

		payload := map[string]string{
			"expand": "version,container",
			"limit":  fmt.Sprintf("%d", pageSize),
			"start":  fmt.Sprintf("%d", start),
		}

		request, err := api.v1().Res(
			"content/"+pageID+"/child/attachment", &result,
		).Get(payload)
		if err != nil {
			return nil, err
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		for i, info := range result.Results {
			if info.Links.Context == "" {
				info.Links.Context = result.Links.Context
			}
			result.Results[i] = info
		}

		all = append(all, result.Results...)

		if len(result.Results) < pageSize || result.Links.Next == "" {
			break
		}

		start += len(result.Results)
	}

	return all, nil
}

func (api *API) GetPageByID(pageID string) (*PageInfo, error) {
	return api.GetPageByIDExpanded(pageID, "ancestors,version")
}

func (api *API) GetPageByIDExpanded(pageID string, expand string) (*PageInfo, error) {
	request, err := api.v1().Res(
		"content/"+pageID, &PageInfo{},
	).Get(map[string]string{"expand": expand})
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*PageInfo), nil
}

func (api *API) GetInlineComments(pageID string) (*InlineComments, error) {
	const pageSize = 100
	all := &InlineComments{}
	start := 0

	for {
		result := &InlineComments{}
		request, err := api.v1().Res(
			"content/"+pageID+"/child/comment", result,
		).Get(map[string]string{
			"expand": "extensions.inlineProperties",
			"limit":  fmt.Sprintf("%d", pageSize),
			"start":  fmt.Sprintf("%d", start),
		})
		if err != nil {
			return nil, err
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		if all.Links.Context == "" {
			all.Links = result.Links
		}

		all.Results = append(all.Results, result.Results...)

		if len(result.Results) < pageSize || result.Links.Next == "" {
			break
		}

		start += len(result.Results)
	}

	return all, nil
}

func (api *API) CreatePage(
	space string,
	pageType string,
	parent *PageInfo,
	title string,
	body string,
) (*PageInfo, error) {
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

	request, err := api.v1().Res(
		"content/", &PageInfo{},
	).Post(payload)
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	page := request.Response.(*PageInfo)

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

	return page, nil
}

func (api *API) UpdatePage(page *PageInfo, newContent string, minorEdit bool, versionMessage string, appearance string, emojiString string) error {
	nextPageVersion := page.Version.Number + 1

	var oldAncestors []map[string]any

	if page.Type != "blogpost" && len(page.Ancestors) > 0 {
		// picking only the last one, which is required by confluence
		oldAncestors = []map[string]any{
			{"id": page.Ancestors[len(page.Ancestors)-1].ID},
		}
	}

	properties := map[string]any{
		// Fix to set full-width as has changed on Confluence APIs again.
		// https://jira.atlassian.com/browse/CONFCLOUD-65447
		//
		"content-appearance-published": map[string]any{
			"value": appearance,
		},
		// content-appearance-draft should not be set as this is impacted by
		// the user editor default configurations - which caused the sporadic published widths.
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

	payload := map[string]any{
		"id":    page.ID,
		"type":  page.Type,
		"title": page.Title,
		"version": map[string]any{
			"number":    nextPageVersion,
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
	if len(oldAncestors) > 0 {
		payload["ancestors"] = oldAncestors
	}

	request, err := api.v1().Res(
		"content/"+page.ID, &map[string]any{},
	).Put(payload)
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return newErrorStatusNotOK(request)
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

	suffix := "\x00" + title + "\x00" + pageType
	for key, entry := range api.pageCache {
		if entry == nil && strings.HasSuffix(key, suffix) {
			delete(api.pageCache, key)
		}
	}
}

func (api *API) AddPageLabels(page *PageInfo, newLabels []string) (*LabelInfo, error) {

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

	request, err := api.v1().Res(
		"content/"+page.ID+"/label", &LabelInfo{},
	).Post(payload)
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*LabelInfo), nil
}

func (api *API) DeletePageLabel(page *PageInfo, label string) (*LabelInfo, error) {

	request, err := api.v1().Res(
		"content/"+page.ID+"/label", &LabelInfo{},
	).SetQuery(map[string]string{"name": label}).Delete()
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*LabelInfo), nil
}

func (api *API) GetPageLabels(page *PageInfo, prefix string) (*LabelInfo, error) {
	type labelPage struct {
		Links struct {
			Next string `json:"next"`
		} `json:"_links"`
		Labels []Label `json:"results"`
		Size   int     `json:"number"`
	}

	const pageSize = 50
	var all []Label
	start := 0

	for {
		var result labelPage

		request, err := api.v1().Res(
			"content/"+page.ID+"/label", &result,
		).Get(map[string]string{
			"prefix": prefix,
			"limit":  fmt.Sprintf("%d", pageSize),
			"start":  fmt.Sprintf("%d", start),
		})
		if err != nil {
			return nil, err
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		all = append(all, result.Labels...)

		if len(result.Labels) < pageSize || result.Links.Next == "" {
			break
		}

		start += len(result.Labels)
	}

	return &LabelInfo{Labels: all, Size: len(all)}, nil
}

// GetUserByName resolves a display name to a Confluence user.
//
// Results are cached for the lifetime of the API value. Every @mention in a
// document renders through the "user" stdlib template func, so an uncached
// lookup meant one CQL search per occurrence -- a document naming the same
// person twenty times issued twenty searches, multiplied by every file in the
// run. Names do not change mid-run, so the lookup is memoised, failures
// included.
func (api *API) GetUserByName(name string) (*User, error) {
	if entry, ok := api.cachedUser(name); ok {
		return entry.user, entry.err
	}

	user, err := api.fetchUserByName(name)

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

func (api *API) fetchUserByName(name string) (*User, error) {
	var response struct {
		Results []struct {
			User User
		}
	}

	// Try the new path first
	request, err := api.v1().
		Res("search").
		Res("user", &response).
		Get(map[string]string{
			"cql": fmt.Sprintf("user.fullname~%q", name),
		})
	if err != nil {
		return nil, err
	}

	// Try old path
	if request.Raw.StatusCode != http.StatusOK || len(response.Results) == 0 {
		request, err = api.v1().
			Res("search", &response).
			Get(map[string]string{
				"cql": fmt.Sprintf("user.fullname~%q", name),
			})
		if err != nil {
			return nil, err
		}
		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}
	}

	if len(response.Results) == 0 {

		return nil, fmt.Errorf("user with name %q is not found", name)
	}

	return &response.Results[0].User, nil
}

func (api *API) GetCurrentUser() (*User, error) {
	var user User

	request, err := api.v1().
		Res("user").
		Res("current", &user).
		Get()
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

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

// IsCloud reports whether the target is Confluence Cloud, probing at most once
// per API value.
//
// The result is memoised through sync.Once rather than a plain bool pair: the
// slow path issues an HTTP request, so two callers racing here would both probe
// and would also write isCloudFlag concurrently. Once also guarantees that a
// caller arriving while the probe is in flight waits for the answer instead of
// reading a half-written one.
func (api *API) IsCloud() bool {
	api.isCloudOnce.Do(func() {
		// 1. Fast path: check for a known Cloud host
		if isCloudHost(api.rest.Api.BaseUrl.Hostname()) {
			api.isCloudFlag = true
			return
		}

		// 2. Slow path: probe Cloud-only v2 API endpoint
		var result any
		request, err := api.v2().Res("spaces", &result).Get(map[string]string{
			"limit": "1",
		})
		api.isCloudFlag = err == nil &&
			(request.Raw.StatusCode == http.StatusOK || request.Raw.StatusCode == http.StatusForbidden)
	})

	return api.isCloudFlag
}

func (api *API) RestrictPageUpdates(
	page *PageInfo,
	allowedUser string,
) error {
	userMap := map[string]any{
		"type": "known",
	}

	if api.IsCloud() {
		user, err := api.GetUserByName(allowedUser)
		if err != nil {
			// Fall back to the currently authenticated user if the specified
			// user cannot be resolved by name.
			currentUser, currentErr := api.GetCurrentUser()
			if currentErr != nil {
				return fmt.Errorf("unable to resolve user %q: %w", allowedUser, err)
			}
			user = currentUser
		}

		if user.AccountID == "" {
			return fmt.Errorf("resolved user %q has no accountId", allowedUser)
		}
		userMap["accountId"] = user.AccountID
	} else {
		userMap["username"] = allowedUser
	}

	var result any
	request, err := api.v1().
		Res("content").
		Id(page.ID).
		Res("restriction", &result).
		Post([]map[string]any{
			{
				"operation": "update",
				"restrictions": map[string]any{
					"user": []map[string]any{
						userMap,
					},
				},
			},
		})
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != http.StatusOK && request.Raw.StatusCode != http.StatusNoContent {
		if !api.IsCloud() && (request.Raw.StatusCode == http.StatusNotFound || request.Raw.StatusCode == http.StatusMethodNotAllowed) {
			return fmt.Errorf("confluence server/datacenter version is too old to support page edit restrictions via REST API (requires Confluence 8.8.0 or newer; status: %d)", request.Raw.StatusCode)
		}
		return newErrorStatusNotOK(request)
	}

	return nil
}

// Folder API methods (Phase 2 implementation)
func (api *API) CreateFolder(spaceID, title string, parentID *string, parentType string) (*FolderInfo, error) {
	actualSpaceID := spaceID

	if parentType == "" {
		parentType = "folder"
	}

	// If we have a folder parent, use the parent's space ID to avoid cross-space conflicts.
	if parentID != nil && parentType == "folder" {
		parentFolder, err := api.GetFolderByID(*parentID)
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

	request, err := api.v2().Res(
		"folders", &FolderInfo{},
	).Post(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create folder %s in space %s: %w", title, spaceID, err)
	}

	if request.Raw.StatusCode != http.StatusOK && request.Raw.StatusCode != http.StatusCreated {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*FolderInfo), nil
}

func (api *API) FindFolder(spaceKey, title, underAncestorID string) (*FolderInfo, error) {
	// CQL folder search lives on /rest/api/search (not content/search).
	result := struct {
		Results []struct {
			Content struct {
				ID    string `json:"id"`
				Type  string `json:"type"`
				Title string `json:"title"`
			} `json:"content"`
		} `json:"results"`
	}{}

	escapedTitle := strings.ReplaceAll(title, `\`, `\\`)
	escapedTitle = strings.ReplaceAll(escapedTitle, `"`, `\"`)
	cql := fmt.Sprintf(`type=folder AND title="%s" AND space="%s"`, escapedTitle, spaceKey)
	if underAncestorID != "" {
		cql += fmt.Sprintf(` AND ancestor=%s`, underAncestorID)
	}

	payload := map[string]string{
		"cql":    cql,
		"limit":  "1",
		"expand": "content",
	}

	request, err := api.v1().Res(
		"search", &result,
	).Get(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to search for folder %s: %w", title, err)
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	if len(result.Results) == 0 || result.Results[0].Content.ID == "" {
		return nil, nil // Folder not found
	}

	item := result.Results[0].Content
	if item.Type != "folder" {
		return nil, nil
	}

	// Found a folder, now get its full details using v2 API
	return api.GetFolderByID(item.ID)
}

func (api *API) GetFolderByID(folderID string) (*FolderInfo, error) {
	request, err := api.v2().Res(
		"folders/"+folderID, &FolderInfo{},
	).Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get folder by ID %s: %w", folderID, err)
	}

	if request.Raw.StatusCode == http.StatusNotFound {
		return nil, nil // Folder not found
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*FolderInfo), nil
}

// GetSpaceID resolves a space key to the numeric id v2 endpoints want.
//
// Cached for the lifetime of the API value, failures included, for the same
// reason as FindHomePage: it is called for every file that has a folder in its
// ancestry and once more per manifest load, and a space key's id does not move
// while a run is going on.
func (api *API) GetSpaceID(spaceKey string) (string, error) {
	if entry, ok := api.cachedSpaceID(spaceKey); ok {
		return entry.id, entry.err
	}

	id, err := api.fetchSpaceID(spaceKey)

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

func (api *API) fetchSpaceID(spaceKey string) (string, error) {
	// Try v1 first: it looks a space up by key directly, where v2 has to be
	// asked for a filtered collection.
	//
	// The response is decoded into SpaceInfo rather than a struct declared
	// here. The local one typed `id` as a string, but v1 returns it as a JSON
	// *number*, so the decode failed on every single call, gopencils returned
	// the error, and this branch was skipped every time -- making the v2
	// "fallback" below the only path that had ever run. SpaceInfo already types
	// the field correctly and is exercised against real Confluence by
	// FindHomePage, so there is no reason for a second, divergent shape.
	//
	// v2 keeps its own string-typed struct: the two APIs genuinely disagree
	// about this field, which is what made the mismatch easy to miss.
	var v1Result SpaceInfo

	request, err := api.v1().Res("space/"+spaceKey, &v1Result).Get()
	if err == nil && request.Raw.StatusCode == http.StatusOK && v1Result.ID != 0 {
		return strconv.Itoa(v1Result.ID), nil
	}

	// Fallback to v2 API with query parameter
	v2Result := struct {
		Results []struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"results"`
	}{}

	payload := map[string]string{
		"keys": spaceKey,
	}

	request, err = api.v2().Res(
		"spaces", &v2Result,
	).Get(payload)
	if err != nil {
		return "", fmt.Errorf("failed to get space ID for key %s (tried both v1 and v2 APIs): %w", spaceKey, err)
	}

	if request.Raw.StatusCode != http.StatusOK {
		return "", newErrorStatusNotOK(request)
	}

	if len(v2Result.Results) == 0 {
		return "", fmt.Errorf("space with key %s not found", spaceKey)
	}

	return v2Result.Results[0].ID, nil
}

// CreatePageWithFolderParent creates a page with a folder as parent using REST API v2
func (api *API) CreatePageWithFolderParent(
	space string,
	pageType string,
	folderID string,
	title string,
	body string,
) (*PageInfo, error) {
	spaceID, err := api.GetSpaceID(space)
	if err != nil {
		return nil, err
	}

	// Using REST API v2 for pages with folder parents
	payload := map[string]interface{}{
		"spaceId":    spaceID,
		"status":     "current",
		"type":       pageType,
		"title":      title,
		"parentId":   folderID,
		"parentType": "folder",
		"body": map[string]interface{}{
			"representation": "storage",
			"value":          body,
		},
	}

	result := &PageInfo{}
	request, err := api.v2().Res("pages", result).Post(payload)
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK && request.Raw.StatusCode != http.StatusCreated {
		return nil, newErrorStatusNotOK(request)
	}

	result.Links.Full = "/pages/viewpage.action?pageId=" + result.ID
	if result.Links.Base == "" {
		result.Links.Base = api.BaseURL
	}
	cacheTitle := title
	if result.Title != "" {
		cacheTitle = result.Title
	}
	cacheType := pageType
	if result.Type != "" {
		cacheType = result.Type
	}
	key := pageCacheKey(space, cacheTitle, cacheType)

	api.lazyInit()
	api.pageCacheMutex.Lock()
	api.setCacheEntry(key, result)
	api.pageCacheMutex.Unlock()

	return result, nil
}

// MoveContentAppend relocates any content (page, folder, etc.) under targetID using the v1 move API.
// GetChildPages returns a page's children in the order Confluence shows them.
//
// The order of this response is the order of the tree in the UI, which is what
// makes it usable for working out which pages are already where they should be.
// Position is not requested explicitly: it exists as an extension field but is
// absent whenever a branch has never been ordered by hand, and the sequence is
// the part that matters here.
func (api *API) GetChildPages(parentID string) ([]PageInfo, error) {
	const pageSize = 100

	var all []PageInfo
	start := 0

	for {
		result := struct {
			Results []PageInfo `json:"results"`
		}{}

		request, err := api.v1().Res(
			"content/"+parentID+"/child/page", &result,
		).Get(map[string]string{
			"limit": fmt.Sprintf("%d", pageSize),
			"start": fmt.Sprintf("%d", start),
		})
		if err != nil {
			return nil, fmt.Errorf("unable to list children of %s: %w", parentID, err)
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		all = append(all, result.Results...)
		if len(result.Results) < pageSize {
			break
		}
		start += len(result.Results)
	}

	return all, nil
}

// MoveContentAfter places a page immediately after one of its siblings.
//
// Unlike the append form, the target here is a sibling rather than the new
// parent. Atlassian warn against using it when the target is a top-level page,
// where it can move content to the root of the space; callers are expected not
// to.
// DeletePage moves a page to the space's trash.
//
// Trash, not oblivion: Confluence keeps a deleted page recoverable until
// somebody purges it, and purging is a second call this deliberately does not
// make. A tool that removes pages because a file left a repository should leave
// the last word to a person.
func (api *API) DeletePage(contentID string) error {
	request, err := api.v1().Res("content").Id(contentID, &struct{}{}).Delete()
	if err != nil {
		return fmt.Errorf("unable to delete content %s: %w", contentID, err)
	}

	// 204 is the documented answer; 200 is accepted because some versions send
	// it, and a page already gone is the outcome that was wanted anyway.
	switch request.Raw.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusNotFound:
		return nil
	default:
		return newErrorStatusNotOK(request)
	}
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
func (api *API) ArchivePage(contentID string) error {
	id, err := strconv.ParseInt(contentID, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot archive content %q: not a page id: %w", contentID, err)
	}

	payload := map[string]any{
		"pages": []map[string]any{{"id": id}},
	}

	request, err := api.v1().Res("content").Res("archive", &struct{}{}).Post(payload)
	if err != nil {
		return fmt.Errorf("unable to archive content %s: %w", contentID, err)
	}

	switch request.Raw.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	default:
		return newErrorStatusNotOK(request)
	}
}

func (api *API) MoveContentAfter(contentID, siblingID string) error {
	return api.moveContent(contentID, "after", siblingID)
}

// MoveContentBefore places a page immediately before one of its siblings. The
// same caution about top-level targets applies as for MoveContentAfter.
func (api *API) MoveContentBefore(contentID, siblingID string) error {
	return api.moveContent(contentID, "before", siblingID)
}

func (api *API) MoveContentAppend(contentID, targetID string) error {
	return api.moveContent(contentID, "append", targetID)
}

func (api *API) moveContent(contentID, position, targetID string) error {
	path := fmt.Sprintf("content/%s/move/%s/%s", contentID, position, targetID)
	var result map[string]any
	request, err := api.v1().Res(path, &result).Put(map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to move content %s %s %s: %w", contentID, position, targetID, err)
	}

	switch request.Raw.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	default:
		return newErrorStatusNotOK(request)
	}
}

// ErrNotFound reports that Confluence answered 404. It is a sentinel so a
// caller can distinguish "the thing is not there" -- which is often an ordinary
// state rather than a failure -- from every other way a request can go wrong.
var ErrNotFound = errors.New("404 (Not Found)")

func newErrorStatusNotOK(request *gopencils.Resource) error {
	defer func() {
		_ = request.Raw.Body.Close()
	}()

	if request.Raw.StatusCode == http.StatusUnauthorized {
		return errors.New(
			"the Confluence API returned unexpected status: 401 (Unauthorized)",
		)
	}

	if request.Raw.StatusCode == http.StatusNotFound {
		// Wrapped rather than a bare string so callers that need to act on
		// "this is gone" specifically can ask with errors.Is instead of
		// matching on the message. The rendered text is unchanged.
		return fmt.Errorf(
			"the Confluence API returned unexpected status: %w", ErrNotFound,
		)
	}

	output, _ := io.ReadAll(request.Raw.Body)

	return fmt.Errorf(
		"the Confluence API returned unexpected status: %v, "+
			"output: %q",
		request.Raw.Status, output,
	)
}
