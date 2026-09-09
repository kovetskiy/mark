package confluence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kovetskiy/gopencils"
)

// Property is a key/value pair Confluence stores against a space or a page.
//
// The value is held as raw JSON because callers own its shape; this package
// stores and returns bytes without interpreting them.
//
// Version matters: Confluence versions properties, and an update must name the
// version it supersedes. A stale one draws a 409, which is how two concurrent
// writers find out about each other instead of silently overwriting.
type Property struct {
	ID      string          `json:"id"`
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

// propertyPageSize is requested when listing properties.
//
// One page is never enough on its own, whatever the caller holds. Properties
// belong to the object, not to the app that wrote them: on Server and Data
// Center every one of mark's manifest shards is a content property of the space
// homepage, sitting next to `editor`, `content-appearance-published`, the
// emoji-title keys and whatever else any installed app has put there. Once the
// total passes a page and a shard falls off the end, the caller sees no
// property, treats every mapping it held as absent, and its next write POSTs a
// key that already exists. Both listings therefore read to the end of the
// collection.
const propertyPageSize = 100

// ErrPropertyConflict reports that a property write lost a race with another
// writer. It is separate from a generic error so a caller can re-read and retry
// on exactly this case and nothing else.
var ErrPropertyConflict = errors.New("property version conflict")

// ErrPropertyUnseen reports that a create collided with a property the listing
// never showed.
//
// This is what an incomplete listing looks like from the write side: the key is
// there, this run did not see it, and everything it held has already been
// treated as absent. The diagnosis a plain conflict gets -- "updated by a
// concurrent run" -- is wrong here and worth saying differently.
//
// Deliberately not an ErrPropertyConflict: a caller that shrugs this off the
// way it shrugs off a lost race loses the same data again on every run
// afterwards. It is still not a reason to abandon the writes that would have
// succeeded -- see how the manifest handles it.
var ErrPropertyUnseen = errors.New("property already exists but was not listed")

// ListSpaceProperties returns every property stored against a space.
//
// Space properties exist only in the v2 API, which means Cloud only. Server and
// Data Center have no equivalent; see ListContentProperties for what stands in
// there.
//
// The whole collection is fetched rather than one key at a time because a
// caller holding several related properties would otherwise pay a request per
// key. A space with none is not an error: the first run of anything that stores
// state this way finds nothing, and that is the normal case.
func (api *API) ListSpaceProperties(spaceID string) ([]Property, error) {
	return api.listPropertiesV2("spaces", spaceID, "space "+spaceID)
}

// nextCursor pulls the cursor out of a v2 _links.next.
//
// v2 paginates by an opaque cursor rather than by offset, and names the next
// page as a whole URL -- relative on some deployments, absolute on others -- of
// which the cursor is the only part worth reusing. Rebuilding the request
// around it rather than following the link keeps this client's base URL and
// headers, which is what makes the loop work through the scoped-token gateway
// as well as against a tenant directly.
func nextCursor(next string) string {
	if next == "" {
		return ""
	}

	parsed, err := url.Parse(next)
	if err != nil {
		return ""
	}

	return parsed.Query().Get("cursor")
}

// missingIsEmpty and missingIsFailure name the two readings of a 404 on the
// first page of a v2 listing.
const (
	// A collection that has never held anything answers 404 rather than an
	// empty page -- what a space or a page with no properties does.
	missingIsEmpty = true
	// A 404 is the object itself being absent, which is worth reporting.
	missingIsFailure = false
)

// listPagedV2 walks a cursor-paged v2 collection, handing each page of results
// to visit. visit returns false to stop before the collection is exhausted.
//
// Every v2 listing pages the same way and carries the same three hazards, which
// is why they are dealt with here rather than once per caller: the collection
// may answer 404 instead of coming back empty, a 404 partway through is a real
// failure that must not be read as "none", and a server that hands back the
// cursor it was given would otherwise keep the loop going for as long as it
// keeps answering.
func listPagedV2[T any](
	api *API,
	path string,
	describe string,
	query map[string]string,
	absentIsEmpty bool,
	visit func([]T) bool,
) error {
	var cursor string

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
			return newTransportError(request, describe, err)
		}

		// First page only: reading a later 404 as "none" would throw away the
		// pages already in hand.
		if absentIsEmpty && cursor == "" && request.Raw.StatusCode == http.StatusNotFound {
			return nil
		}

		if request.Raw.StatusCode != http.StatusOK {
			return newErrorStatusNotOK(request)
		}

		if !visit(result.Results) {
			return nil
		}

		next := nextCursor(result.Links.Next)
		if next == "" || next == cursor || len(result.Results) == 0 {
			return nil
		}
		cursor = next
	}
}

// collectPagedV2 is listPagedV2 for a caller that wants the whole collection.
func collectPagedV2[T any](
	api *API,
	path string,
	describe string,
	query map[string]string,
	absentIsEmpty bool,
) ([]T, error) {
	var all []T

	err := listPagedV2(api, path, describe, query, absentIsEmpty, func(page []T) bool {
		all = append(all, page...)
		return true
	})
	if err != nil {
		return nil, err
	}

	return all, nil
}

// listPropertiesV2 returns every property stored against one v2 object.
//
// owner names it for an error message -- "space 123", "page 456" -- since the
// collection and the id read as an URL rather than as English.
func (api *API) listPropertiesV2(collection, ownerID, owner string) ([]Property, error) {
	return collectPagedV2[Property](
		api,
		collection+"/"+ownerID+"/properties",
		"read properties of "+owner,
		map[string]string{"limit": strconv.Itoa(propertyPageSize)},
		missingIsEmpty,
	)
}

// setPropertyV2 writes value to a property of one v2 object, creating it if
// absent.
func (api *API) setPropertyV2(
	collection, ownerID, owner, key string,
	value []byte,
	existing *Property,
) error {
	payload := map[string]any{
		"key":   key,
		"value": json.RawMessage(value),
	}

	var (
		result  Property
		request *gopencils.Resource
		err     error
	)

	object := api.v2().Res(collection).Res(ownerID)

	if existing == nil {
		// The result pointer goes on the collection resource itself: routing it
		// through a further Res("") would append a trailing slash, which this
		// API is not reliably forgiving about.
		request, err = object.Res("properties", &result).Post(payload)
	} else {
		// v2 addresses an update by property id.
		payload["version"] = map[string]any{"number": existing.Version.Number + 1}
		request, err = object.Res("properties").Res(existing.ID, &result).Put(payload)
	}
	if err != nil {
		return newTransportError(
			request, fmt.Sprintf("write property %q of %s", key, owner), err,
		)
	}

	return propertyWriteResult(request, key, owner, existing == nil)
}

// SetSpaceProperty writes value to a space property, creating it if absent.
func (api *API) SetSpaceProperty(spaceID, key string, value []byte, existing *Property) error {
	return api.setPropertyV2("spaces", spaceID, "space "+spaceID, key, value, existing)
}

// ListPageProperties returns every property stored against a page, through v2.
//
// The v1 form of this is ListContentProperties, which is the one to use
// everywhere v1 answers: it exists on Server and Data Center too. This one is
// for the scoped-token path, where v1 is refused and the properties a page
// update carries -- content appearance, emoji title -- have nowhere else to go.
func (api *API) ListPageProperties(pageID string) ([]Property, error) {
	return api.listPropertiesV2("pages", pageID, "page "+pageID)
}

// SetPageProperty writes value to a page property through v2, creating it if
// absent. See ListPageProperties for when this is the right API to use.
func (api *API) SetPageProperty(pageID, key string, value []byte, existing *Property) error {
	return api.setPropertyV2("pages", pageID, "page "+pageID, key, value, existing)
}

// ListContentProperties returns every property stored against a page.
//
// Unlike space properties this is a v1 endpoint, present on Server and Data
// Center as well as Cloud, which is what makes it usable as the storage of last
// resort on a non-Cloud instance.
func (api *API) ListContentProperties(contentID string) ([]Property, error) {
	var all []Property
	start := 0

	for {
		var result struct {
			Results []Property `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		request, err := api.v1().
			Res("content").
			Res(contentID).
			Res("property", &result).
			Get(map[string]string{
				"limit": strconv.Itoa(propertyPageSize),
				"start": strconv.Itoa(start),
			})
		if err != nil {
			return nil, newTransportError(request, "read properties of content "+contentID, err)
		}

		// Only the first page may read 404 as "none set"; one partway through
		// is a failure, and swallowing it would discard the pages already read.
		if request.Raw.StatusCode == http.StatusNotFound && start == 0 {
			return nil, nil
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		all = append(all, result.Results...)

		// Two ways a listing says it is finished, and a deployment may use
		// either. Confluence caps limit at its max-results setting and answers
		// a capped request with a short page *and* a next link, so a short page
		// alone does not mean the end -- that is how the tail of a listing was
		// being thrown away, silently, on exactly the instances configured to
		// hand out less than they were asked for. Other deployments omit the
		// link entirely and only ever go short.
		//
		// So: keep going while either says there is more, and stop on an empty
		// page whatever they say, which is what keeps a server that does
		// neither from being asked for the same offset forever.
		if len(result.Results) == 0 ||
			(result.Links.Next == "" && len(result.Results) < propertyPageSize) {
			break
		}
		start += len(result.Results)
	}

	return all, nil
}

// SetContentProperty writes value to a page property, creating it if absent.
func (api *API) SetContentProperty(contentID, key string, value []byte, existing *Property) error {
	payload := map[string]any{
		"key":   key,
		"value": json.RawMessage(value),
	}

	var (
		result  Property
		request *gopencils.Resource
		err     error
	)

	content := api.v1().Res("content").Res(contentID)

	if existing == nil {
		request, err = content.Res("property", &result).Post(payload)
	} else {
		// v1 addresses an update by key in the path, but still wants the
		// property id in the body, and the version being moved to rather than
		// the one being replaced. Atlassian documents both as required; omitting
		// the id is the kind of thing a fake will happily accept and a real
		// instance will not.
		payload["id"] = existing.ID
		payload["version"] = map[string]any{"number": existing.Version.Number + 1}
		request, err = content.Res("property").Res(key, &result).Put(payload)
	}
	if err != nil {
		return newTransportError(
			request, fmt.Sprintf("write property %q of content %s", key, contentID), err,
		)
	}

	return propertyWriteResult(request, key, "content "+contentID, existing == nil)
}

// propertyWriteResult turns a property write response into an error, singling
// out the version conflict so a caller can react to losing a race and nothing
// else.
//
// creating says whether the caller believed the key was absent, which is what
// tells the two ways a 409 arrives apart. Losing a race to another writer is
// ordinary and survivable. Colliding with a key that was there the whole time
// is neither: it means the listing this run worked from was incomplete, so
// whatever that property held has already been treated as missing.
func propertyWriteResult(request *gopencils.Resource, key, subject string, creating bool) error {
	switch request.Raw.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusConflict:
		if creating {
			return fmt.Errorf(
				"property %q of %s already exists but was not in the listing this run read, "+
					"so anything it held has been treated as absent: %w",
				key, subject, ErrPropertyUnseen,
			)
		}
		return fmt.Errorf(
			"property %q of %s was modified concurrently: %w",
			key, subject, ErrPropertyConflict,
		)
	default:
		return newErrorStatusNotOK(request)
	}
}
