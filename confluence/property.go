package confluence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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

// propertyPageSize is requested when listing properties. Callers here hold a
// small fixed number of them, so one page is always enough and no pagination
// loop is needed; asking explicitly keeps that true regardless of what the
// server's default page size happens to be.
const propertyPageSize = "100"

// ErrPropertyConflict reports that a property write lost a race with another
// writer. It is separate from a generic error so a caller can re-read and retry
// on exactly this case and nothing else.
var ErrPropertyConflict = errors.New("property version conflict")

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
	var result struct {
		Results []Property `json:"results"`
	}

	request, err := api.restV2.
		Res("spaces").
		Res(spaceID).
		Res("properties", &result).
		Get(map[string]string{"limit": propertyPageSize})
	if err != nil {
		return nil, fmt.Errorf("unable to read properties of space %s: %w", spaceID, err)
	}

	// A space with no properties at all answers 404 rather than an empty
	// collection, so both shapes have to mean "none set".
	if request.Raw.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return result.Results, nil
}

// SetSpaceProperty writes value to a space property, creating it if absent.
func (api *API) SetSpaceProperty(spaceID, key string, value []byte, existing *Property) error {
	payload := map[string]any{
		"key":   key,
		"value": json.RawMessage(value),
	}

	var (
		result  Property
		request *gopencils.Resource
		err     error
	)

	space := api.restV2.Res("spaces").Res(spaceID)

	if existing == nil {
		// The result pointer goes on the collection resource itself: routing it
		// through a further Res("") would append a trailing slash, which this
		// API is not reliably forgiving about.
		request, err = space.Res("properties", &result).Post(payload)
	} else {
		// v2 addresses an update by property id.
		payload["version"] = map[string]any{"number": existing.Version.Number + 1}
		request, err = space.Res("properties").Res(existing.ID, &result).Put(payload)
	}
	if err != nil {
		return fmt.Errorf("unable to write property %q of space %s: %w", key, spaceID, err)
	}

	return propertyWriteResult(request, key, "space "+spaceID)
}

// ListContentProperties returns every property stored against a page.
//
// Unlike space properties this is a v1 endpoint, present on Server and Data
// Center as well as Cloud, which is what makes it usable as the storage of last
// resort on a non-Cloud instance.
func (api *API) ListContentProperties(contentID string) ([]Property, error) {
	var result struct {
		Results []Property `json:"results"`
	}

	request, err := api.rest.
		Res("content").
		Res(contentID).
		Res("property", &result).
		Get(map[string]string{"limit": propertyPageSize})
	if err != nil {
		return nil, fmt.Errorf("unable to read properties of content %s: %w", contentID, err)
	}

	if request.Raw.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return result.Results, nil
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

	content := api.rest.Res("content").Res(contentID)

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
		return fmt.Errorf("unable to write property %q of content %s: %w", key, contentID, err)
	}

	return propertyWriteResult(request, key, "content "+contentID)
}

// propertyWriteResult turns a property write response into an error, singling
// out the version conflict so a caller can react to losing a race and nothing
// else.
func propertyWriteResult(request *gopencils.Resource, key, subject string) error {
	switch request.Raw.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusConflict:
		return fmt.Errorf(
			"property %q of %s was modified concurrently: %w",
			key, subject, ErrPropertyConflict,
		)
	default:
		return newErrorStatusNotOK(request)
	}
}
