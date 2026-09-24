package confluence

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
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
	SpaceID    string `json:"spaceId"`

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
		ID:     content.ID,
		Title:  content.Title,
		Type:   pageType,
		Status: content.Status,
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

// listV2 collects every result of a cursor-paged v2 listing.
//
// A 404 on the first page is read as an empty collection: that is how v2
// answers for a space or page that has never had a property. Later on, a 404
// is a failure like any other, since the results already in hand would
// otherwise be thrown away.
func listV2[T any](api *API, path string, query map[string]string, describe string) ([]T, error) {
	var (
		all    []T
		cursor string
	)

	for {
		var result struct {
			Results []T `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		page := make(map[string]string, len(query)+1)
		for key, value := range query {
			page[key] = value
		}
		if cursor != "" {
			page["cursor"] = cursor
		}

		request, err := api.v2().Res(path, &result).Get(page)
		if err != nil {
			return nil, newTransportError(request, describe, err)
		}

		if request.Raw.StatusCode == http.StatusNotFound && cursor == "" {
			return nil, nil
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		all = append(all, result.Results...)

		next := nextCursor(result.Links.Next)
		// A server that hands back the cursor it was given would otherwise
		// keep this loop going for as long as it keeps answering.
		if next == "" || next == cursor || len(result.Results) == 0 {
			return all, nil
		}
		cursor = next
	}
}

// findPageV2 is FindPage and findPageWithStatus against v2: a title within a
// space, in one status. v2 filters by space id where v1 filters by key.
func (api *API) findPageV2(space, title, pageType, status string) (*PageInfo, error) {
	spaceID, err := api.GetSpaceID(space)
	if err != nil {
		return nil, err
	}

	query := map[string]string{
		"space-id": spaceID,
		"status":   status,
	}
	if title != "" {
		query["title"] = title
	}

	// One request, not a paged walk: as on v1, the first result is the answer.
	var result struct {
		Results []contentV2 `json:"results"`
		Links   struct {
			Base string `json:"base"`
		} `json:"_links"`
	}

	request, err := api.v2().Res(v2Collection(pageType), &result).Get(query)
	if err != nil {
		return nil, newTransportError(
			request, fmt.Sprintf("find page %q in space %s", title, space), err,
		)
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	api.learnSiteBase(result.Links.Base)

	if len(result.Results) == 0 {
		return nil, nil
	}

	page := api.pageInfoV2(result.Results[0], pageType)
	if page.Status == "" {
		page.Status = status
	}

	page.Ancestors, err = api.ancestorsV2(result.Results[0])
	if err != nil {
		return nil, err
	}

	return page, nil
}

// readContentV2 reads one object out of a v2 collection.
func (api *API) readContentV2(collection, id string, withBody bool) (*contentV2, error) {
	content, _, err := api.readContentStatusV2(collection, id, withBody)
	return content, err
}

// readContentStatusV2 is readContentV2 that also hands back the status the
// server answered with, or 0 when there was no answer.
func (api *API) readContentStatusV2(collection, id string, withBody bool) (*contentV2, int, error) {
	var result contentV2

	query := map[string]string{}
	if withBody {
		query["body-format"] = "storage"
	}

	request, err := api.v2().Res(collection+"/"+id, &result).Get(query)
	if err != nil {
		status := 0
		if request != nil && request.Raw != nil {
			status = request.Raw.StatusCode
		}
		return nil, status, newTransportError(request, "read "+collection+" "+id, err)
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, request.Raw.StatusCode, newErrorStatusNotOK(request)
	}

	return &result, request.Raw.StatusCode, nil
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
func (api *API) lookupContentV2(id string, withBody bool) (*contentV2, string, error) {
	first, second := "page", "blogpost"
	if known, ok := api.contentTypesV2.Load(id); ok && known == "blogpost" {
		first, second = second, first
	}

	content, status, err := api.readContentStatusV2(v2Collection(first), id, withBody)
	if status == http.StatusNotFound {
		other, otherStatus, otherErr := api.readContentStatusV2(v2Collection(second), id, withBody)
		if otherErr == nil {
			return other, second, nil
		}
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
func (api *API) collectionOfV2(id string) (string, error) {
	if known, ok := api.contentTypesV2.Load(id); ok {
		return v2Collection(known.(string)), nil
	}

	_, pageType, err := api.lookupContentV2(id, false)
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
func (api *API) getPageByIDV2(pageID, expand string) (*PageInfo, error) {
	content, pageType, err := api.lookupContentV2(pageID, strings.Contains(expand, "body.storage"))
	if err != nil {
		return nil, err
	}

	page := api.pageInfoV2(*content, pageType)

	if strings.Contains(expand, "ancestors") {
		page.Ancestors, err = api.ancestorsV2(*content)
		if err != nil {
			return nil, err
		}
	}

	// v2 names the space by id; the key, which is what v1 expands and what a
	// document names, costs a read of the space.
	if strings.Contains(expand, "space") && content.SpaceID != "" {
		page.Space.Key, err = api.spaceKeyV2(content.SpaceID)
		if err != nil {
			return nil, err
		}
	}

	return page, nil
}

// spaceKeyV2 is the key of the space with this v2 id.
func (api *API) spaceKeyV2(spaceID string) (string, error) {
	var result struct {
		Key string `json:"key"`
	}

	request, err := api.v2().Res("spaces/"+spaceID, &result).Get()
	if err != nil {
		return "", newTransportError(request, "read space "+spaceID, err)
	}

	if request.Raw.StatusCode != http.StatusOK {
		return "", newErrorStatusNotOK(request)
	}

	return result.Key, nil
}

// ancestorsV2 rebuilds the ancestor chain v1 hands over for free: v2 names the
// immediate parent only, so the chain is walked a level at a time.
//
// It has to be rebuilt rather than left empty, because mark decides where a
// page belongs by comparing its ancestors' titles against the ancestry the
// document declares, and a page with no ancestors reads as one sitting at the
// root of its space. A parent that is not a page ends the walk: folders are not
// ancestors, which is also how the v1 path behaves.
func (api *API) ancestorsV2(content contentV2) ([]ancestor, error) {
	var chain []ancestor

	parentID, parentType := content.ParentID, content.ParentType
	for parentID != "" && (parentType == "" || parentType == "page") && len(chain) < maxAncestorDepth {
		parent, err := api.readContentV2("pages", parentID, false)
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
func (api *API) createPageV2(space, pageType, parentID, parentType, title, body string) (*PageInfo, error) {
	spaceID, err := api.GetSpaceID(space)
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

	var result contentV2

	request, err := api.v2().Res(v2Collection(pageType), &result).Post(payload)
	if err != nil {
		return nil, newTransportError(
			request, fmt.Sprintf("create page %q in space %s", title, space), err,
		)
	}

	if request.Raw.StatusCode != http.StatusOK && request.Raw.StatusCode != http.StatusCreated {
		return nil, api.explainCreateFailure(space, title, pageType, newErrorStatusNotOK(request))
	}

	return api.pageInfoV2(result, pageType), nil
}

// updatePageV2 is the content half of UpdatePage against v2.
//
// v1 carries the content appearance and the emoji title inside the update, as
// metadata properties. v2 keeps properties on an endpoint of their own, so
// UpdatePage writes them afterwards with setPagePropertiesV2.
func (api *API) updatePageV2(
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

	request, err := api.v2().Res(v2Collection(page.Type)+"/"+page.ID, &map[string]any{}).Put(payload)
	if err != nil {
		return newTransportError(
			request, fmt.Sprintf("update page %q (%s)", page.Title, page.ID), err,
		)
	}

	if request.Raw.StatusCode != http.StatusOK {
		return newErrorStatusNotOK(request)
	}

	return nil
}

// setPagePropertiesV2 writes the properties v1 would have carried inside the
// page update: a map of key to {"value": ...}, as UpdatePage builds it. A
// blogpost's properties live under /blogposts, and /pages answers 404 for it.
func (api *API) setPagePropertiesV2(page *PageInfo, properties map[string]any) error {
	collection, pageID := v2Collection(page.Type), page.ID

	existing, err := api.listPropertiesV2(collection, pageID)
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

		if err := api.setPropertyV2(collection, pageID, key, value, byKey[key]); err != nil {
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
func (api *API) getAttachmentsV2(pageID string) ([]AttachmentInfo, error) {
	type attachmentV2 struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Comment      string `json:"comment"`
		DownloadLink string `json:"downloadLink"`
	}

	collection, err := api.collectionOfV2(pageID)
	if err != nil {
		return nil, fmt.Errorf("unable to list attachments of page %s: %w", pageID, err)
	}

	found, err := listV2[attachmentV2](
		api, collection+"/"+pageID+"/attachments", map[string]string{"limit": "250"},
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
func (api *API) getPageLabelsV2(page *PageInfo, prefix string) (*LabelInfo, error) {
	type labelV2 struct {
		ID     json.Number `json:"id"`
		Name   string      `json:"name"`
		Prefix string      `json:"prefix"`
	}

	query := map[string]string{"limit": "250"}
	if prefix != "" {
		query["prefix"] = prefix
	}

	found, err := listV2[labelV2](
		api, v2Collection(page.Type)+"/"+page.ID+"/labels", query, "read labels of page "+page.ID,
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
