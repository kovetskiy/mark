package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// This file is the v2 form of the page calls that mark otherwise makes against
// v1. It exists for Atlassian's scoped API tokens.
//
// A scoped token only works through the api.atlassian.com gateway, and the
// gateway checks the token's granular scopes against the endpoint being
// called. The v2 endpoints check the page scopes such a token is minted with
// (read:page:confluence, write:page:confluence); the v1 endpoints check the
// older content ones, which the token does not have. So /rest/api/content
// answers
//
//	401 {"code":401,"message":"Unauthorized; scope does not match"}
//
// while /api/v2/pages answers perfectly well. That is issue #917.
//
// Rather than try v1 and fall back on a refusal, the choice is made once from
// the base URL: through the gateway, the page calls go to v2 and nowhere else.
// Every other deployment keeps v1 exactly as it was -- v1 answers in one
// request what v2 needs several for -- and a classic token through the gateway
// is entitled to v2 as well, so nothing is lost by not asking v1 there.

// isGatewayURL reports whether baseURL points at the api.atlassian.com gateway
// (https://api.atlassian.com/ex/confluence/<cloudId>), which is the only route
// a scoped API token can take to Confluence.
func isGatewayURL(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsed.Hostname(), "api.atlassian.com") ||
		strings.HasPrefix(parsed.Path, "/ex/confluence/")
}

// maxAncestorDepth bounds the parent walk in ancestorsV2, so that a server
// answering with a cycle cannot keep it going forever.
const maxAncestorDepth = 100

// contentV2 is a page or blogpost as v2 returns it: the parent is a single id
// rather than an ancestor chain, and the body only comes when asked for.
type contentV2 struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Title      string `json:"title"`
	ParentID   string `json:"parentId"`
	ParentType string `json:"parentType"`

	Version struct {
		Number  int64  `json:"number"`
		Message string `json:"message"`
	} `json:"version"`

	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`

	Links struct {
		WebUI string `json:"webui"`
		Base  string `json:"base"`
	} `json:"_links"`
}

// pageInfoV2 converts a v2 object into the shape the rest of mark reads. The
// type comes from the caller: v2 tells pages and blogposts apart by collection.
func (api *API) pageInfoV2(content contentV2, pageType string) *PageInfo {
	page := &PageInfo{
		ID:         content.ID,
		Title:      content.Title,
		Type:       pageType,
		Status:     content.Status,
		ParentID:   content.ParentID,
		ParentType: content.ParentType,
	}
	page.Version.Number = content.Version.Number
	page.Version.Message = content.Version.Message
	page.Body.Storage.Value = content.Body.Storage.Value
	page.Links.Full = content.Links.WebUI

	api.contentTypesV2.Store(content.ID, pageType)
	api.learnSiteBase(content.Links.Base)
	page.Links.Base = api.siteBaseURL()

	return page
}

// learnSiteBase records the site URL a v2 response named in _links.base.
func (api *API) learnSiteBase(base string) {
	if base == "" {
		return
	}

	api.siteBaseMutex.Lock()
	api.siteBase = strings.TrimSuffix(base, "/")
	api.siteBaseMutex.Unlock()
}

// siteBaseURL is what a link to a page should start with.
//
// Through the gateway the configured base URL is an API host, and a tiny link
// built from it is one a browser cannot open (#1019). Confluence names the
// site's own URL in the _links.base of its v2 listings -- the space lookup
// that opens every run among them -- and that is what is used once it has
// been seen. Until then the configured URL stands, as it does on v1.
func (api *API) siteBaseURL() string {
	api.siteBaseMutex.RLock()
	defer api.siteBaseMutex.RUnlock()

	if api.siteBase != "" {
		return api.siteBase
	}

	return api.BaseURL
}

// v2Collection names the v2 collection holding a v1 content type.
func v2Collection(pageType string) string {
	if pageType == "blogpost" {
		return "blogposts"
	}

	return "pages"
}

// listV1 collects every result of an offset-paged v1 listing, pageSize at a
// time. visit, when it is not nil, sees each page before its results are
// collected, and may change them.
//
// With notFoundEmpty, a 404 on the first page is read as an empty listing.
// Only the first: a 404 partway through is a failure, and swallowing it would
// discard the pages already read.
func listV1[T any](
	ctx context.Context,
	api *API,
	path []string,
	query url.Values,
	pageSize int,
	describe string,
	notFoundEmpty bool,
	visit func(page *listing[T]),
) ([]T, error) {
	var all []T
	start := 0

	for {
		page := maps.Clone(query)
		if page == nil {
			page = url.Values{}
		}
		page.Set("limit", strconv.Itoa(pageSize))
		page.Set("start", strconv.Itoa(start))

		result, err := get[listing[T]](ctx, api.v1, path, page)
		if notFoundEmpty && errors.Is(err, ErrNotFound) && start == 0 {
			return nil, nil
		}
		if err != nil {
			return nil, requestError(describe, err)
		}

		if visit != nil {
			visit(&result)
		}

		all = append(all, result.Results...)

		// A short page does not mean the end: Confluence caps limit at its
		// max-results setting and answers a capped request with a short page
		// and a next link both. Nor does a missing link, since deployments that
		// omit it only ever go short. Stop when neither says there is more, and
		// on an empty page whatever they say, which is what keeps a server that
		// does neither from being asked for the same offset forever.
		if len(result.Results) == 0 ||
			(result.Links.Next == "" && len(result.Results) < pageSize) {
			return all, nil
		}

		start += len(result.Results)
	}
}

// listV2 collects every result of a cursor-paged v2 listing; see eachV2.
func listV2[T any](ctx context.Context, api *API, path []string, query url.Values, describe string) ([]T, error) {
	var all []T

	err := eachV2(ctx, api, path, query, describe, func(result T) bool {
		all = append(all, result)

		return false
	})
	if err != nil {
		return nil, err
	}

	return all, nil
}

// eachV2 hands visit every result of a cursor-paged v2 listing in turn, and
// stops early once visit says to.
//
// A 404 on the first page is read as an empty collection: that is how v2
// answers for a space or page that has never had a property, and for a
// deployment without the route at all. Later on, a 404 is a failure like any
// other, since the results already in hand would otherwise be thrown away.
func eachV2[T any](
	ctx context.Context,
	api *API,
	path []string,
	query url.Values,
	describe string,
	visit func(result T) (stop bool),
) error {
	var cursor string

	for {
		page := maps.Clone(query)
		if page == nil {
			page = url.Values{}
		}
		if cursor != "" {
			page.Set("cursor", cursor)
		}

		result, err := get[listing[T]](ctx, api.v2, path, page)
		if errors.Is(err, ErrNotFound) && cursor == "" {
			return nil
		}
		if err != nil {
			return requestError(describe, err)
		}

		for _, item := range result.Results {
			if visit(item) {
				return nil
			}
		}

		next := nextCursor(result.Links.Next)
		// A server that hands back the cursor it was given would otherwise
		// keep this loop going for as long as it keeps answering.
		if next == "" || next == cursor || len(result.Results) == 0 {
			return nil
		}
		cursor = next
	}
}

// findPageV2 is FindPage and findPageWithStatus against v2: a title within a
// space, in one status. v2 filters by space id where v1 filters by key.
func (api *API) findPageV2(ctx context.Context, space, title, pageType, status string) (*PageInfo, error) {
	spaceID, err := api.GetSpaceID(ctx, space)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		"space-id": {spaceID},
		"status":   {status},
	}
	if title != "" {
		query.Set("title", title)
	}

	// One request, not a paged walk: as on v1, the first result is the answer.
	result, err := get[listing[contentV2]](ctx, api.v2, []string{v2Collection(pageType)}, query)
	if err != nil {
		return nil, requestError(fmt.Sprintf("find page %q in space %s", title, space), err)
	}

	api.learnSiteBase(result.Links.Base)

	if len(result.Results) == 0 {
		return nil, nil
	}

	page := api.pageInfoV2(result.Results[0], pageType)
	if page.Status == "" {
		page.Status = status
	}

	page.Ancestors, err = api.ancestorsV2(ctx, result.Results[0])
	if err != nil {
		return nil, err
	}

	return page, nil
}

// readContentV2 reads one object out of a v2 collection.
func (api *API) readContentV2(ctx context.Context, collection, id string, withBody bool) (*contentV2, error) {
	query := url.Values{}
	if withBody {
		query.Set("body-format", "storage")
	}

	result, err := get[contentV2](ctx, api.v2, []string{collection, id}, query)
	if err != nil {
		return nil, requestError("read "+collection+" "+id, err)
	}

	return &result, nil
}

// lookupContentV2 reads a page or blogpost known only by its id.
//
// v2 keeps the two in separate collections and answers 404 for an id asked of
// the wrong one, while v1 serves both from /content. So the collection the id
// was last seen in is asked first -- the page one when it has not been seen --
// and a 404 there is followed by a read of the other.
//
// A 404 or a refusal of that second read leaves the first 404 standing. A
// token minted with the page scopes and not the blogpost ones is refused
// /blogposts outright, and that says nothing about the page that was not found
// -- which callers act on as gone rather than as a failure.
func (api *API) lookupContentV2(ctx context.Context, id string, withBody bool) (*contentV2, string, error) {
	first, second := "page", "blogpost"
	if known, ok := api.contentTypesV2.Load(id); ok && known == "blogpost" {
		first, second = second, first
	}

	content, err := api.readContentV2(ctx, v2Collection(first), id, withBody)
	if errors.Is(err, ErrNotFound) {
		other, otherErr := api.readContentV2(ctx, v2Collection(second), id, withBody)
		if otherErr == nil {
			return other, second, nil
		}
		otherStatus := statusOf(otherErr)
		refused := otherStatus == http.StatusNotFound ||
			otherStatus == http.StatusUnauthorized || otherStatus == http.StatusForbidden
		if !refused {
			err = otherErr
		}
	}
	if err != nil {
		return nil, "", err
	}

	return content, first, nil
}

// collectionOfV2 names the v2 collection holding the content with this id,
// for the calls that are handed an id alone. mark reaches every page it
// touches through a find, a create or a read by id first, so the answer is
// normally already known and costs nothing; otherwise it costs a read.
func (api *API) collectionOfV2(ctx context.Context, id string) (string, error) {
	if known, ok := api.contentTypesV2.Load(id); ok {
		return v2Collection(known.(string)), nil
	}

	_, pageType, err := api.lookupContentV2(ctx, id, false)
	if err != nil {
		return "", err
	}
	api.contentTypesV2.Store(id, pageType)

	return v2Collection(pageType), nil
}

// getPageByIDV2 is GetPageByIDExpanded against v2.
//
// The expand list is honoured: v2 charges for both of the things it names, the
// body by a wider response and the ancestors by a request per level.
func (api *API) getPageByIDV2(ctx context.Context, pageID, expand string) (*PageInfo, error) {
	content, pageType, err := api.lookupContentV2(ctx, pageID, strings.Contains(expand, "body.storage"))
	if err != nil {
		return nil, err
	}

	page := api.pageInfoV2(*content, pageType)

	if strings.Contains(expand, "ancestors") {
		page.Ancestors, err = api.ancestorsV2(ctx, *content)
		if err != nil {
			return nil, err
		}
	}

	return page, nil
}

// ParentOfV2 reads the direct parent of a page or blogpost from v2: its id and
// whether it is a page or a folder.
//
// It is the one way to see a folder parent from a page read through v1, whose
// ancestors leave folders out. Cloud only: Server and Data Center serve no v2.
func (api *API) ParentOfV2(ctx context.Context, id string) (string, string, error) {
	content, pageType, err := api.lookupContentV2(ctx, id, false)
	if err != nil {
		return "", "", fmt.Errorf("unable to read the parent of %s: %w", id, err)
	}
	api.contentTypesV2.Store(id, pageType)

	return content.ParentID, content.ParentType, nil
}

// ancestorsV2 rebuilds the ancestor chain v1 hands over for free: v2 names the
// immediate parent only, so the chain is walked a level at a time.
//
// It has to be rebuilt rather than left empty, because mark decides where a
// page belongs by comparing its ancestors' titles against the ancestry the
// document declares, and a page with no ancestors reads as one sitting at the
// root of its space. A parent that is not a page ends the walk: folders are not
// ancestors, which is also how the v1 path behaves.
func (api *API) ancestorsV2(ctx context.Context, content contentV2) ([]ancestor, error) {
	var chain []ancestor

	parentID, parentType := content.ParentID, content.ParentType
	for parentID != "" && (parentType == "" || parentType == "page") && len(chain) < maxAncestorDepth {
		parent, err := api.readContentV2(ctx, "pages", parentID, false)
		if err != nil {
			return nil, fmt.Errorf("unable to read ancestor %s of page %s: %w", parentID, content.ID, err)
		}

		chain = append(chain, ancestor{ID: parent.ID, Title: parent.Title})
		parentID, parentType = parent.ParentID, parent.ParentType
	}

	// Walked leaf-first; Confluence lists ancestors root-first, and everything
	// reading the chain takes the last entry as the parent.
	slices.Reverse(chain)

	return chain, nil
}

// ancestor is the element type of PageInfo.Ancestors. An alias, so that a slice
// of it is assignable to that field.
type ancestor = struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// createPageV2 is CreatePage against v2, and CreatePageWithFolderParent always.
//
// parentType is sent only when given: "folder" for a folder parent, which is
// what CreatePageWithFolderParent has always named, and nothing for a page,
// v2's default.
func (api *API) createPageV2(ctx context.Context, space, pageType, parentID, parentType, title, body string) (*PageInfo, error) {
	spaceID, err := api.GetSpaceID(ctx, space)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"spaceId": spaceID,
		"status":  "current",
		"title":   title,
		"body": map[string]any{
			"representation": "storage",
			"value":          body,
		},
	}

	// A blogpost has no parent, and naming one is rejected rather than ignored.
	if parentID != "" && pageType != "blogpost" {
		payload["parentId"] = parentID
		if parentType != "" {
			payload["parentType"] = parentType
		}
	}

	result, err := send[contentV2](ctx, api.v2, http.MethodPost, []string{v2Collection(pageType)}, payload)
	if statusOf(err) != 0 {
		return nil, api.explainCreateFailure(ctx, space, title, pageType, err)
	}
	if err != nil {
		return nil, requestError(fmt.Sprintf("create page %q in space %s", title, space), err)
	}

	return api.pageInfoV2(result, pageType), nil
}

// updatePageV2 is the content half of UpdatePage against v2.
//
// v1 carries the content appearance and the emoji title inside the update, as
// metadata properties. v2 keeps properties on an endpoint of their own, so
// UpdatePage writes them afterwards with setPagePropertiesV2.
func (api *API) updatePageV2(
	ctx context.Context,
	page *PageInfo,
	newContent string,
	minorEdit bool,
	versionMessage string,
	nextVersion int64,
) error {
	payload := map[string]any{
		"id":     page.ID,
		"status": "current",
		"title":  page.Title,
		"body": map[string]any{
			"representation": "storage",
			"value":          newContent,
		},
		"version": map[string]any{
			"number":    nextVersion,
			"minorEdit": minorEdit,
			"message":   versionMessage,
		},
	}

	// As on v1, the parent goes in only when there is one to name: naming none
	// is how v2 is asked to move a page to the root of its space.
	if page.Type != "blogpost" && len(page.Ancestors) > 0 {
		payload["parentId"] = page.Ancestors[len(page.Ancestors)-1].ID
	}

	_, err := api.v2.do(
		ctx, nil, http.MethodPut, []string{v2Collection(page.Type), page.ID}, nil, payload, &map[string]any{},
	)
	if err != nil {
		return requestError(fmt.Sprintf("update page %q (%s)", page.Title, page.ID), err)
	}

	return nil
}

// setPagePropertiesV2 writes the properties v1 would have carried inside the
// page update: a map of key to {"value": ...}, as UpdatePage builds it. A
// blogpost's properties live under /blogposts, and /pages answers 404 for it.
func (api *API) setPagePropertiesV2(ctx context.Context, page *PageInfo, properties map[string]any) error {
	collection, pageID := v2Collection(page.Type), page.ID

	existing, err := api.listPropertiesV2(ctx, collection, pageID)
	if err != nil {
		return fmt.Errorf("unable to read properties of page %s: %w", pageID, err)
	}

	byKey := map[string]*Property{}
	for i := range existing {
		byKey[existing[i].Key] = &existing[i]
	}

	// In key order, so that a failure part way through leaves the page in the
	// same state every time.
	for _, key := range slices.Sorted(maps.Keys(properties)) {
		value, err := json.Marshal(properties[key].(map[string]any)["value"])
		if err != nil {
			return fmt.Errorf("unable to encode property %q of page %s: %w", key, pageID, err)
		}

		if err := api.setPropertyV2(ctx, collection, pageID, key, value, byKey[key]); err != nil {
			return err
		}
	}

	return nil
}

// getAttachmentsV2 is GetAttachments against v2. Only the listing has a v2
// form; uploading stays on v1, which has the only endpoint for it.
//
// GetAttachments is handed an id alone, and asking the wrong collection is not
// an error here but an empty listing -- listV2 reads a 404 as one -- so the
// collection is worked out first rather than guessed.
func (api *API) getAttachmentsV2(ctx context.Context, pageID string) ([]AttachmentInfo, error) {
	type attachmentV2 struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Comment      string `json:"comment"`
		DownloadLink string `json:"downloadLink"`
	}

	collection, err := api.collectionOfV2(ctx, pageID)
	if err != nil {
		return nil, fmt.Errorf("unable to list attachments of page %s: %w", pageID, err)
	}

	found, err := listV2[attachmentV2](
		ctx, api, []string{collection, pageID, "attachments"}, url.Values{"limit": {"250"}},
		"list attachments of page "+pageID,
	)
	if err != nil {
		return nil, err
	}

	attachments := make([]AttachmentInfo, 0, len(found))
	for _, attachment := range found {
		info := AttachmentInfo{Filename: attachment.Title, ID: attachment.ID}
		// The checksum that tells an unchanged attachment apart lives in the
		// comment, which v2 keeps at the top level rather than under metadata.
		info.Metadata.Comment = attachment.Comment
		// v2 gives the download link without its context; Cloud, which is all
		// the gateway ever fronts, always serves under /wiki.
		info.Links.Context = "/wiki"
		info.Links.Download = attachment.DownloadLink

		attachments = append(attachments, info)
	}

	return attachments, nil
}

// getPageLabelsV2 is GetPageLabels against v2, which hands the label id over as
// a number where v1 makes it a string.
func (api *API) getPageLabelsV2(ctx context.Context, page *PageInfo, prefix string) (*LabelInfo, error) {
	type labelV2 struct {
		ID     json.Number `json:"id"`
		Name   string      `json:"name"`
		Prefix string      `json:"prefix"`
	}

	query := url.Values{"limit": {"250"}}
	if prefix != "" {
		query.Set("prefix", prefix)
	}

	found, err := listV2[labelV2](
		ctx, api, []string{v2Collection(page.Type), page.ID, "labels"}, query, "read labels of page "+page.ID,
	)
	if err != nil {
		return nil, err
	}

	labels := make([]Label, 0, len(found))
	for _, label := range found {
		labels = append(labels, Label{ID: label.ID.String(), Name: label.Name, Prefix: label.Prefix})
	}

	return &LabelInfo{Labels: labels, Size: len(labels)}, nil
}
