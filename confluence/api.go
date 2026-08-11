package confluence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"resty.dev/v3"
)

type User struct {
	AccountID string `json:"accountId,omitempty"`
	UserKey   string `json:"userKey,omitempty"`
	Username  string `json:"username,omitempty"`
}

type API struct {
	rest *resty.Client
	// v2 API for newer endpoints like folders
	restV2  *resty.Client
	BaseURL string

	isCloudFlag bool
	isCloudOnce sync.Once

	pageCache      map[string]*PageInfo
	pageCacheByID  map[string]*PageInfo
	pageCacheMutex sync.RWMutex

	userCache      map[string]userCacheEntry
	userCacheMutex sync.RWMutex
}

// userCacheEntry records the outcome of a user lookup, including a failed one:
// a document that mentions an unknown name would otherwise re-query for every
// occurrence, which is the case the cache is least able to afford.
type userCacheEntry struct {
	user *User
	err  error
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

// tracer adapts zerolog to resty.Logger. Resty logs at three levels; all of
// them are routed to trace here because the client is only given a logger when
// mark itself is running at trace level.
type tracer struct {
	prefix string
}

func (t *tracer) Errorf(format string, args ...any) { t.logf(format, args...) }
func (t *tracer) Warnf(format string, args ...any)  { t.logf(format, args...) }
func (t *tracer) Debugf(format string, args ...any) { t.logf(format, args...) }

func (t *tracer) logf(format string, args ...any) {
	log.Trace().Msgf(t.prefix+" "+format, args...)
}

func NewAPI(baseURL string, username string, password string, insecureSkipVerify bool) *API {
	// Normalize baseURL once before building all derived endpoints.
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Both clients share one http.Client so they share the retry transport,
	// the phase timeouts and the cookie jar. Resty's own retry is deliberately
	// left off: retryTransport already retries, and it is method-aware, where
	// resty's default conditions would replay a failed POST and risk creating
	// a page twice.
	httpClient := newHTTPClient(insecureSkipVerify)

	newClient := func(suffix, prefix string) *resty.Client {
		client := resty.NewWithClient(httpClient).SetBaseURL(baseURL + suffix)
		if username != "" {
			client.SetBasicAuth(username, password)
		} else {
			client.SetAuthToken(password)
		}
		if zerolog.GlobalLevel() == zerolog.TraceLevel {
			client.SetDebug(true).SetLogger(&tracer{prefix})
		}
		return client
	}

	rest := newClient("/rest/api", "rest:")
	restV2 := newClient("/api/v2", "rest-v2:") // v2 API for folders and new features

	return &API{
		rest:          rest,
		restV2:        restV2,
		BaseURL:       baseURL,
		pageCache:     make(map[string]*PageInfo),
		pageCacheByID: make(map[string]*PageInfo),
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

func (api *API) FindHomePage(space string) (*PageInfo, error) {
	payload := map[string]string{
		"expand": "homepage",
	}

	var spaceInfo SpaceInfo
	resp, err := api.rest.R().
		SetResult(&spaceInfo).
		SetQueryParams(payload).
		Get("space/" + space)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
	}

	return &spaceInfo.Homepage, nil
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

	resp, err := api.rest.R().
		SetResult(&result).
		SetQueryParams(payload).
		Get("content/")
	if err != nil {
		return nil, err
	}

	// allow 404 because it's fine if page is not found,
	// the function will return nil, nil
	if resp.StatusCode() != http.StatusNotFound && resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
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

	var result struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}

	// Resty builds the multipart body, so the form no longer has to be
	// assembled by hand, and the Authorization header no longer has to be
	// rescued from a cleared header map.
	resp, err := api.rest.R().
		SetResult(&result).
		SetFileReader("file", name, reader).
		SetMultipartFormData(map[string]string{"comment": comment}).
		SetHeader("X-Atlassian-Token", "no-check").
		Post("content/" + pageID + "/child/attachment")
	if err != nil {
		return info, err
	}

	if resp.StatusCode() != http.StatusOK {
		return info, newErrorStatusNotOK(resp)
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

	var extendedResponse struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}

	resp, err := api.rest.R().
		SetFileReader("file", name, reader).
		SetMultipartFormData(map[string]string{"comment": comment}).
		SetHeader("X-Atlassian-Token", "no-check").
		Post("content/" + pageID + "/child/attachment/" + attachID + "/data")
	if err != nil {
		return info, err
	}

	if resp.StatusCode() != http.StatusOK {
		return info, newErrorStatusNotOK(resp)
	}

	// Confluence returns either the extended {results:[...]} envelope or a
	// bare attachment object here, so the body is decoded by hand rather than
	// through SetResult.
	result := resp.Bytes()

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

		resp, err := api.rest.R().
			SetResult(&result).
			SetQueryParams(payload).
			Get("content/" + pageID + "/child/attachment")
		if err != nil {
			return nil, err
		}

		if resp.StatusCode() != http.StatusOK {
			return nil, newErrorStatusNotOK(resp)
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
	var pageInfo PageInfo
	resp, err := api.rest.R().
		SetResult(&pageInfo).
		SetQueryParams(map[string]string{"expand": expand}).
		Get("content/" + pageID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
	}

	return &pageInfo, nil
}

func (api *API) GetInlineComments(pageID string) (*InlineComments, error) {
	const pageSize = 100
	all := &InlineComments{}
	start := 0

	for {
		result := &InlineComments{}
		resp, err := api.rest.R().
			SetResult(result).
			SetQueryParams(map[string]string{
				"expand": "extensions.inlineProperties",
				"limit":  fmt.Sprintf("%d", pageSize),
				"start":  fmt.Sprintf("%d", start),
			}).
			Get("content/" + pageID + "/child/comment")
		if err != nil {
			return nil, err
		}

		if resp.StatusCode() != http.StatusOK {
			return nil, newErrorStatusNotOK(resp)
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

	var pageInfo PageInfo
	resp, err := api.rest.R().
		SetResult(&pageInfo).
		SetBody(payload).
		Post("content/")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
	}

	page := &pageInfo

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
	oldAncestors := []map[string]any{}

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
		"ancestors": oldAncestors,
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

	resp, err := api.rest.R().
		SetResult(&map[string]any{}).
		SetBody(payload).
		Put("content/" + page.ID)
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return newErrorStatusNotOK(resp)
	}

	page.Version.Number = nextPageVersion
	api.updateCachedPageVersion(page.ID, nextPageVersion)
	return nil
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

	var labelInfo LabelInfo
	resp, err := api.rest.R().
		SetResult(&labelInfo).
		SetBody(payload).
		Post("content/" + page.ID + "/label")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
	}

	return &labelInfo, nil
}

func (api *API) DeletePageLabel(page *PageInfo, label string) (*LabelInfo, error) {

	var labelInfo LabelInfo
	resp, err := api.rest.R().
		SetResult(&labelInfo).
		SetQueryParams(map[string]string{"name": label}).
		Delete("content/" + page.ID + "/label")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusNoContent {
		return nil, nil
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
	}

	return &labelInfo, nil
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

		resp, err := api.rest.R().
			SetResult(&result).
			SetQueryParams(map[string]string{
				"prefix": prefix,
				"limit":  fmt.Sprintf("%d", pageSize),
				"start":  fmt.Sprintf("%d", start),
			}).
			Get("content/" + page.ID + "/label")
		if err != nil {
			return nil, err
		}

		if resp.StatusCode() != http.StatusOK {
			return nil, newErrorStatusNotOK(resp)
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
	resp, err := api.rest.R().
		SetResult(&response).
		SetQueryParams(map[string]string{
			"cql": fmt.Sprintf("user.fullname~%q", name),
		}).
		Get("search/user")
	if err != nil {
		return nil, err
	}

	// Try old path
	if resp.StatusCode() != http.StatusOK || len(response.Results) == 0 {
		resp, err = api.rest.R().
			SetResult(&response).
			SetQueryParams(map[string]string{
				"cql": fmt.Sprintf("user.fullname~%q", name),
			}).
			Get("search")
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, newErrorStatusNotOK(resp)
		}
	}

	if len(response.Results) == 0 {

		return nil, fmt.Errorf("user with name %q is not found", name)
	}

	return &response.Results[0].User, nil
}

func (api *API) GetCurrentUser() (*User, error) {
	var user User

	resp, err := api.rest.R().
		SetResult(&user).
		Get("user/current")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
	}

	return &user, nil
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
		// 1. Fast path: check default domain suffix.
		// The previous HTTP layer exposed a pre-parsed base URL; resty does
		// not, so the normalised string on the API value is parsed here. A
		// malformed base URL just falls through to the probe below rather
		// than failing the call.
		var host string
		if parsed, err := url.Parse(api.BaseURL); err == nil {
			host = parsed.Hostname()
		}
		if strings.HasSuffix(host, "jira.com") || strings.HasSuffix(host, "atlassian.net") {
			api.isCloudFlag = true
			return
		}

		// 2. Slow path: probe Cloud-only v2 API endpoint
		var result any
		resp, err := api.restV2.R().
			SetResult(&result).
			SetQueryParams(map[string]string{"limit": "1"}).
			Get("spaces")
		api.isCloudFlag = err == nil &&
			(resp.StatusCode() == http.StatusOK || resp.StatusCode() == http.StatusForbidden)
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
	resp, err := api.rest.R().
		SetResult(&result).
		SetBody([]map[string]any{
			{
				"operation": "update",
				"restrictions": map[string]any{
					"user": []map[string]any{
						userMap,
					},
				},
			},
		}).
		Post("content/" + page.ID + "/restriction")
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		if !api.IsCloud() && (resp.StatusCode() == http.StatusNotFound || resp.StatusCode() == http.StatusMethodNotAllowed) {
			return fmt.Errorf("confluence server/datacenter version is too old to support page edit restrictions via REST API (requires Confluence 8.8.0 or newer; status: %d)", resp.StatusCode())
		}
		return newErrorStatusNotOK(resp)
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

		if parentFolder.SpaceID != "" {
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

	var folderInfo FolderInfo
	resp, err := api.restV2.R().
		SetResult(&folderInfo).
		SetBody(payload).
		Post("folders")
	if err != nil {
		return nil, fmt.Errorf("failed to create folder %s in space %s: %w", title, spaceID, err)
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
		return nil, newErrorStatusNotOK(resp)
	}

	return &folderInfo, nil
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

	resp, err := api.rest.R().
		SetResult(&result).
		SetQueryParams(payload).
		Get("search")
	if err != nil {
		return nil, fmt.Errorf("failed to search for folder %s: %w", title, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
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
	var folderInfo FolderInfo
	resp, err := api.restV2.R().
		SetResult(&folderInfo).
		Get("folders/" + folderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get folder by ID %s: %w", folderID, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil // Folder not found
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorStatusNotOK(resp)
	}

	return &folderInfo, nil
}

func (api *API) GetSpaceID(spaceKey string) (string, error) {
	// Try v1 API first (more reliable for space lookup by key)
	v1Result := struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}{}

	resp, err := api.rest.R().SetResult(&v1Result).Get("space/" + spaceKey)
	if err == nil && resp.StatusCode() == http.StatusOK {
		return v1Result.ID, nil
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

	resp, err = api.restV2.R().
		SetResult(&v2Result).
		SetQueryParams(payload).
		Get("spaces")
	if err != nil {
		return "", fmt.Errorf("failed to get space ID for key %s (tried both v1 and v2 APIs): %w", spaceKey, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", newErrorStatusNotOK(resp)
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
	resp, err := api.restV2.R().SetResult(result).SetBody(payload).Post("pages")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
		return nil, newErrorStatusNotOK(resp)
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
func (api *API) MoveContentAppend(contentID, targetID string) error {
	path := fmt.Sprintf("content/%s/move/append/%s", contentID, targetID)
	var result map[string]any
	resp, err := api.rest.R().SetResult(&result).SetBody(map[string]interface{}{}).Put(path)
	if err != nil {
		return fmt.Errorf("failed to move content %s under %s: %w", contentID, targetID, err)
	}

	switch resp.StatusCode() {
	case http.StatusOK, http.StatusNoContent:
		return nil
	default:
		return newErrorStatusNotOK(resp)
	}
}

func newErrorStatusNotOK(resp *resty.Response) error {
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		return errors.New(
			"the Confluence API returned unexpected status: 401 (Unauthorized)",
		)
	case http.StatusNotFound:
		return errors.New(
			"the Confluence API returned unexpected status: 404 (Not Found)",
		)
	}

	return fmt.Errorf(
		"the Confluence API returned unexpected status: %v, "+
			"output: %q",
		resp.Status(), resp.String(),
	)
}
