package confluence

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/kovetskiy/gopencils"
)

// This file holds the v2 half of every call that mark makes against v1 and can
// also make against v2.
//
// The reason it exists is Atlassian's scoped API tokens. A scoped token carries
// granular scopes -- read:page:confluence, write:page:confluence and the like
// -- and reaches Confluence through the api.atlassian.com gateway. The gateway
// checks the scopes of the token against the endpoint being called, and the v1
// endpoints are not covered by the page scopes: they check the older
// content-shaped ones (read:content-details:confluence,
// write:content:confluence). A token minted for pages therefore gets
//
//	401 {"code":401,"message":"Unauthorized; scope does not match"}
//	X-Failure-Category: FAILURE_CLIENT_SCOPE_CHECK
//
// out of /rest/api/content while /api/v2/pages answers perfectly well. That is
// issue #917: FindHomePage already fell back to v2 (#341), so the run got past
// the space lookup and then died on the very next call.
//
// v1 stays the first choice everywhere. Its answers carry ancestors and a body
// without extra round trips, and a classic token is entitled to it. v2 is asked
// only once v1 has refused, and only where a v2 exists to ask -- Server and
// Data Center have none, so nothing changes there at all, nor for anybody whose
// token v1 accepts.

// maxAncestorDepth bounds the parent walk that rebuilds a v2 ancestor chain.
//
// v2 names only the immediate parent, so the chain costs a request per level.
// The bound is not about a plausible page tree -- it is about not walking
// forever if an instance ever answers with a cycle this walk's seen-set cannot
// already see.
const maxAncestorDepth = 100

// v1Refused reports whether a v1 response is the gateway turning the endpoint
// down rather than Confluence answering about the content.
//
// 401 and 403 are what the scope check produces. 404 is included because that
// is what the v1 space endpoint answers a scoped token with -- the case #341
// was reported as -- and because a deployment that does not route v1 at all
// looks the same from here. The cost of reading a genuine 404 as a refusal is
// one extra request that answers the same way; the cost of not reading a
// refusal as one is the run aborting.
func v1Refused(request *gopencils.Resource) bool {
	if request == nil || request.Raw == nil {
		return false
	}

	switch request.Raw.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	}

	return false
}

// v2Available reports whether this deployment has a v2 API to fall back to.
//
// Server and Data Center do not. Every /api/v2 path there is a 404 from a route
// that was never going to exist, so asking costs a request per call and buries
// the v1 answer that actually explains the failure underneath one from a path
// nobody asked about -- which is worse than useless when the two disagree about
// what kind of failure it was: a v1 403 paired with a v2 404 reads as
// ErrNotFound, and "this page is gone" is a conclusion --on-orphan acts on.
//
// IsCloud is the test mark already applies to its other Cloud-only features. It
// answers without a request at all for the hosts a scoped API token goes
// through, and otherwise probes once for the life of the API value.
func (api *API) v2Available() bool {
	return api.IsCloud()
}

// v1FailedAndSoDidV2 reports a v1 failure together with the v2 attempt that
// followed it.
//
// Only the v1 error is wrapped. The v2 one contributes its text and nothing
// else, deliberately: a fallback that answers 404 -- which is what any /api/v2
// path does on a deployment that has none -- would otherwise make
// errors.Is(err, ErrNotFound) true for a v1 failure that was nothing of the
// kind, and "this page is gone" is a conclusion --on-orphan acts on.
func v1FailedAndSoDidV2(v1Err, v2Err error) error {
	//nolint:errorlint // the v2 error is rendered rather than wrapped on purpose; see above.
	return fmt.Errorf("v1 API: %w (v2 fallback also failed: %s)", v1Err, v2Err)
}

// ancestorRef is the shape PageInfo.Ancestors holds. It is an alias rather than
// a definition so that a value of it can be assigned to the anonymous struct
// type that field is declared with.
type ancestorRef = struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// contentV2 is a page or blogpost as v2 returns it.
//
// The differences from PageInfo that matter: the space is an id rather than a
// key, the parent is a single id rather than an ancestor chain, and the body is
// keyed by the representation that was asked for.
type contentV2 struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Title      string `json:"title"`
	SpaceID    string `json:"spaceId"`
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
	} `json:"_links"`
}

// pageInfo converts a v2 content object into the shape the rest of mark reads.
//
// pageType comes from the caller because v2 says nothing about it: the type is
// which collection the object was fetched from.
func (content contentV2) pageInfo(pageType, baseURL string) *PageInfo {
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
	page.Links.Base = baseURL

	return page
}

// v2Collection names the v2 collection that holds a v1 content type.
func v2Collection(pageType string) string {
	if pageType == "blogpost" {
		return "blogposts"
	}

	return "pages"
}

// findContentV2 is FindPage against v2: a title within a space, in one status.
//
// v2 filters by space id where v1 filters by space key, so this pays for a
// space lookup -- one per space per run, since GetSpaceID caches -- before it
// can ask its own question.
func (api *API) findContentV2(space, title, pageType, status string) (*PageInfo, error) {
	spaceID, err := api.GetSpaceID(space)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve space %s for the v2 page lookup: %w", space, err)
	}

	var result struct {
		Results []contentV2 `json:"results"`
	}

	query := map[string]string{
		"space-id": spaceID,
		"status":   status,
		"limit":    "250",
	}
	if title != "" {
		query["title"] = title
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

	if len(result.Results) == 0 {
		return nil, nil
	}

	page := result.Results[0].pageInfo(pageType, api.BaseURL)

	ancestors, err := api.ancestorsV2(result.Results[0])
	if err != nil {
		return nil, err
	}
	page.Ancestors = ancestors

	return page, nil
}

// getContentV2 reads one page or blogpost by id.
//
// A page id and a blogpost id live in different v2 collections, and the callers
// of GetPageByID hold an id without always knowing which. Rather than make them
// say, a 404 from pages is retried against blogposts -- the same two-step v1
// spared everybody by having a single /content collection.
func (api *API) getContentV2(contentID string, withBody bool) (*contentV2, string, error) {
	for _, collection := range []string{"pages", "blogposts"} {
		var result contentV2

		query := map[string]string{}
		if withBody {
			query["body-format"] = "storage"
		}

		request, err := api.v2().Res(collection+"/"+contentID, &result).Get(query)
		if err != nil {
			return nil, "", newTransportError(request, "read page "+contentID, err)
		}

		if request.Raw.StatusCode == http.StatusNotFound && collection == "pages" {
			continue
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, "", newErrorStatusNotOK(request)
		}

		pageType := "page"
		if collection == "blogposts" {
			pageType = "blogpost"
		}

		return &result, pageType, nil
	}

	return nil, "", fmt.Errorf("no content with id %s: %w", contentID, ErrNotFound)
}

// getPageByIDV2 is GetPageByIDExpanded against v2.
//
// The v1 expand list is honoured rather than ignored: v2 charges for both of
// the things it names -- the body by a wider response, the ancestors by a
// request per level -- and every caller that wants neither would otherwise pay
// for both.
func (api *API) getPageByIDV2(pageID, expand string) (*PageInfo, error) {
	content, pageType, err := api.getContentV2(pageID, strings.Contains(expand, "body.storage"))
	if err != nil {
		return nil, err
	}

	page := content.pageInfo(pageType, api.BaseURL)

	if strings.Contains(expand, "ancestors") {
		ancestors, err := api.ancestorsV2(*content)
		if err != nil {
			return nil, err
		}
		page.Ancestors = ancestors
	}

	return page, nil
}

// ancestorsV2 rebuilds the ancestor chain v1 hands over for free.
//
// v2 names the immediate parent and nothing above it, so the chain is walked a
// level at a time. It is worth the requests: mark decides where a page belongs
// by comparing the titles of its ancestors against the ancestry the document
// declares, and a page that arrives with no ancestors at all reads as one
// sitting at the root of the space -- which is an invitation to move it there.
//
// A parent that is not a page ends the walk. Folders are v2 entities that a
// page can sit in, and Confluence does not count them as ancestors; UpdatePage
// relies on that same rule when it decides whether an update names a parent.
func (api *API) ancestorsV2(content contentV2) ([]ancestorRef, error) {
	var chain []ancestorRef

	seen := map[string]bool{content.ID: true}
	parentID, parentType := content.ParentID, content.ParentType

	for parentID != "" && (parentType == "" || parentType == "page") {
		if seen[parentID] || len(chain) >= maxAncestorDepth {
			break
		}
		seen[parentID] = true

		parent, _, err := api.getContentV2(parentID, false)
		if err != nil {
			// An ancestor that cannot be read at all is reported: answering
			// with a chain known to be short would tell the caller the page
			// sits somewhere it does not.
			return nil, fmt.Errorf("unable to read ancestor %s: %w", parentID, err)
		}

		chain = append(chain, ancestorRef{ID: parent.ID, Title: parent.Title})
		parentID, parentType = parent.ParentID, parent.ParentType
	}

	// The walk collects leaf-first; Confluence returns ancestors root-first,
	// and everything reading the chain takes the last entry as the parent.
	slices.Reverse(chain)

	return chain, nil
}

// cloudContextPath is where Confluence Cloud serves itself under, and what v1
// reports as an attachment's link context.
//
// v2 gives a download link without one. Hardcoding it is safe in exactly this
// spot: v2 is Cloud-only, and Cloud is always mounted at /wiki. On Server and
// Data Center -- the deployments that mount elsewhere -- there is no v2 for
// this to be reached through.
const cloudContextPath = "/wiki"

// getAttachmentsV2 is GetAttachments against v2.
//
// Only the listing has a v2 form. Uploading an attachment is a v1 endpoint with
// no v2 equivalent at all, which is why mark asks for the classic content
// scopes in its documentation rather than pretending v2 is enough for
// everything.
func (api *API) getAttachmentsV2(pageID string) ([]AttachmentInfo, error) {
	const pageSize = 100

	var all []AttachmentInfo
	var cursor string

	for {
		var result struct {
			Results []struct {
				ID           string `json:"id"`
				Title        string `json:"title"`
				Comment      string `json:"comment"`
				DownloadLink string `json:"downloadLink"`
			} `json:"results"`

			Links struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		query := map[string]string{"limit": fmt.Sprintf("%d", pageSize)}
		if cursor != "" {
			query["cursor"] = cursor
		}

		request, err := api.v2().Res("pages/"+pageID+"/attachments", &result).Get(query)
		if err != nil {
			return nil, newTransportError(request, "list attachments of page "+pageID, err)
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		for _, attachment := range result.Results {
			info := AttachmentInfo{Filename: attachment.Title, ID: attachment.ID}
			// The checksum mark recognises an unchanged attachment by lives in
			// the comment, which v2 keeps at the top level rather than under
			// metadata.
			info.Metadata.Comment = attachment.Comment
			info.Links.Context = cloudContextPath
			info.Links.Download = attachment.DownloadLink

			all = append(all, info)
		}

		next := nextCursor(result.Links.Next)
		// A server that hands back the cursor it was given would otherwise keep
		// this loop going for as long as it keeps answering.
		if next == "" || next == cursor || len(result.Results) == 0 {
			break
		}
		cursor = next
	}

	return all, nil
}

// getPageLabelsV2 is GetPageLabels against v2.
//
// The id is decoded as a number and rendered back as a string: v1 hands labels
// over with a string id and v2 with an integer one, and everything downstream
// reads the name rather than the id.
func (api *API) getPageLabelsV2(pageID, prefix string) (*LabelInfo, error) {
	const pageSize = 50

	var all []Label
	var cursor string

	for {
		var result struct {
			Results []struct {
				ID     json.Number `json:"id"`
				Name   string      `json:"name"`
				Prefix string      `json:"prefix"`
			} `json:"results"`

			Links struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		query := map[string]string{"limit": fmt.Sprintf("%d", pageSize)}
		if prefix != "" {
			query["prefix"] = prefix
		}
		if cursor != "" {
			query["cursor"] = cursor
		}

		request, err := api.v2().Res("pages/"+pageID+"/labels", &result).Get(query)
		if err != nil {
			return nil, newTransportError(request, "read labels of page "+pageID, err)
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		for _, label := range result.Results {
			all = append(all, Label{
				ID:     label.ID.String(),
				Name:   label.Name,
				Prefix: label.Prefix,
			})
		}

		next := nextCursor(result.Links.Next)
		// A server that hands back the cursor it was given would otherwise keep
		// this loop going for as long as it keeps answering.
		if next == "" || next == cursor || len(result.Results) == 0 {
			break
		}
		cursor = next
	}

	return &LabelInfo{Labels: all, Size: len(all)}, nil
}

// createContentV2 is CreatePage against v2.
func (api *API) createContentV2(
	space string,
	pageType string,
	parent *PageInfo,
	title string,
	body string,
) (*PageInfo, error) {
	spaceID, err := api.GetSpaceID(space)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve space %s for the v2 page create: %w", space, err)
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
	if parent != nil && pageType != "blogpost" {
		payload["parentId"] = parent.ID
	}

	var result contentV2

	request, err := api.v2().Res(v2Collection(pageType), &result).Post(payload)
	if err != nil {
		return nil, newTransportError(
			request, fmt.Sprintf("create page %q in space %s", title, space), err,
		)
	}

	if request.Raw.StatusCode != http.StatusOK && request.Raw.StatusCode != http.StatusCreated {
		return nil, newErrorStatusNotOK(request)
	}

	return result.pageInfo(pageType, api.BaseURL), nil
}

// updateContentV2 is UpdatePage against v2.
//
// The one thing v2 cannot do in the same request is the metadata v1 carries in
// `metadata.properties`: the published content appearance and the emoji title
// are content properties, and v2 keeps properties on an endpoint of their own.
// They are written afterwards, by the caller, so that a page whose content went
// in is not reported as unpublished because its width did not.
func (api *API) updateContentV2(
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

	// As on v1, the parent goes in only when there is one to name: a page whose
	// parent is a folder has no ancestors, and naming no parent at all is how
	// v2 is asked to move a page to the root of its space.
	if page.Type != "blogpost" && len(page.Ancestors) > 0 {
		payload["parentId"] = page.Ancestors[len(page.Ancestors)-1].ID
	}

	request, err := api.v2().Res(
		v2Collection(page.Type)+"/"+page.ID, &map[string]any{},
	).Put(payload)
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

// propertyValues flattens the shape v1 takes properties in -- a key naming a
// {"value": ...} object -- into the key/value pairs the v2 property API wants.
//
// Anything whose value is not a string is dropped rather than guessed at: every
// property mark sets is a string, and inventing an encoding for one that is not
// would be writing something nobody asked for.
func propertyValues(properties map[string]any) map[string]string {
	values := make(map[string]string, len(properties))

	for key, wrapper := range properties {
		object, ok := wrapper.(map[string]any)
		if !ok {
			continue
		}

		if value, ok := object["value"].(string); ok {
			values[key] = value
		}
	}

	return values
}

// setContentPropertiesV2 writes the properties that v1 carries inside the page
// update itself.
//
// Each key is read before it is written because a property update has to name
// the version it supersedes, and creating one that already exists is a
// conflict rather than an overwrite.
func (api *API) setContentPropertiesV2(pageID string, properties map[string]string) error {
	existing, err := api.ListPageProperties(pageID)
	if err != nil {
		return fmt.Errorf("unable to read properties of page %s: %w", pageID, err)
	}

	byKey := map[string]*Property{}
	for i, property := range existing {
		byKey[property.Key] = &existing[i]
	}

	// Sorted, so that a failure part way through leaves the same page in the
	// same state every time rather than one of two.
	for _, key := range slices.Sorted(maps.Keys(properties)) {
		value, err := json.Marshal(properties[key])
		if err != nil {
			return fmt.Errorf("unable to encode property %q: %w", key, err)
		}

		if err := api.SetPageProperty(pageID, key, value, byKey[key]); err != nil {
			return err
		}
	}

	return nil
}
