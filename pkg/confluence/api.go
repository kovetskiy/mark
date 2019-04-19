package confluence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/bndr/gopencils"
	"github.com/kovetskiy/lorg"
	"github.com/reconquest/cog"
	"github.com/reconquest/karma-go"
)

func discarder() *lorg.Log {
	stderr := lorg.NewLog()
	stderr.SetOutput(ioutil.Discard)
	return stderr
}

var (
	log = cog.NewLogger(discarder())
)

func SetLogger(logger *cog.Logger) {
	log = logger
}

type RestrictionOperation string

const (
	RestrictionEdit RestrictionOperation = `Edit`
	RestrictionView                      = `View`
)

type Restriction struct {
	User  string `json:"userName"`
	Group string `json:"groupName",omitempty`
}

type API struct {
	rest *gopencils.Resource

	// it's deprecated accordingly to Atlassian documentation,
	// but it's only way to set permissions
	json *gopencils.Resource
}

type PageInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	Version struct {
		Number int64 `json:"number"`
	} `json:"version"`

	Ancestors []struct {
		Id    string `json:"id"`
		Title string `json:"title"`
	} `json:"ancestors"`

	Links struct {
		Full string `json:"webui"`
	} `json:"_links"`
}

func NewAPI(baseURL string, username string, password string) *API {
	auth := &gopencils.BasicAuth{username, password}

	return &API{
		rest: gopencils.Api(baseURL+"/rest/api", auth),

		json: gopencils.Api(
			baseURL+"/rpc/json-rpc/confluenceservice-v2",
			auth,
		),
	}
}

func (api *API) FindRootPage(space string) (*PageInfo, error) {
	page, err := api.FindPage(space, ``)
	if err != nil {
		return nil, karma.Format(
			err,
			"can't obtain first page from space %q",
			space,
		)
	}

	if page == nil {
		return nil, errors.New("no such space")
	}

	if len(page.Ancestors) == 0 {
		return nil, fmt.Errorf(
			"page %q from space %q has no parents",
			page.Title,
			space,
		)
	}

	return &PageInfo{
		ID:    page.Ancestors[0].Id,
		Title: page.Ancestors[0].Title,
	}, nil
}

func (api *API) FindPage(space string, title string) (*PageInfo, error) {
	result := struct {
		Results []PageInfo `json:"results"`
	}{}

	payload := map[string]string{
		"spaceKey": space,
		"expand":   "ancestors,version",
	}

	if title != "" {
		payload["title"] = title
	}

	request, err := api.rest.Res(
		"content/", &result,
	).Get(payload)
	if err != nil {
		return nil, err
	}

	// allow 404 because it's fine if page is not found,
	// the function will return nil, nil
	if request.Raw.StatusCode != 404 && request.Raw.StatusCode != 200 {
		return nil, newErrorStatusNotOK(request)
	}

	if len(result.Results) == 0 {
		return nil, nil
	}

	return &result.Results[0], nil
}

func (api *API) GetAttachments(pageID string) error {
	result := map[string]interface{}{}

	payload := map[string]string{
		"expand": "version,container",
	}

	request, err := api.rest.Res(
		"content/"+pageID+"/child/attachment", &result,
	).Get(payload)
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != 200 {
		return newErrorStatusNotOK(request)
	}

	{
		marshaledXXX, _ := json.MarshalIndent(result, "", "  ")
		fmt.Printf("result: %s\n", string(marshaledXXX))
	}

	return nil
}

func (api *API) GetPageByID(pageID string) (*PageInfo, error) {
	request, err := api.rest.Res(
		"content/"+pageID, &PageInfo{},
	).Get(map[string]string{"expand": "ancestors,version"})
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != 200 {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*PageInfo), nil
}

func (api *API) CreatePage(
	space string,
	parent *PageInfo,
	title string,
	body string,
) (*PageInfo, error) {
	payload := map[string]interface{}{
		"type":  "page",
		"title": title,
		"space": map[string]interface{}{
			"key": space,
		},
		"body": map[string]interface{}{
			"storage": map[string]interface{}{
				"representation": "storage",
				"value":          body,
			},
		},
	}

	if parent != nil {
		payload["ancestors"] = []map[string]interface{}{
			{"id": parent.ID},
		}
	}

	request, err := api.rest.Res(
		"content/", &PageInfo{},
	).Post(payload)
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != 200 {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*PageInfo), nil
}

func (api *API) UpdatePage(
	page *PageInfo, newContent string,
) error {
	nextPageVersion := page.Version.Number + 1

	if len(page.Ancestors) == 0 {
		return fmt.Errorf(
			"page %q info does not contain any information about parents",
			page.ID,
		)
	}

	// picking only the last one, which is required by confluence
	oldAncestors := []map[string]interface{}{
		{"id": page.Ancestors[len(page.Ancestors)-1].Id},
	}

	payload := map[string]interface{}{
		"id":    page.ID,
		"type":  "page",
		"title": page.Title,
		"version": map[string]interface{}{
			"number":    nextPageVersion,
			"minorEdit": false,
		},
		"ancestors": oldAncestors,
		"body": map[string]interface{}{
			"storage": map[string]interface{}{
				"value":          string(newContent),
				"representation": "storage",
			},
		},
	}

	request, err := api.rest.Res(
		"content/"+page.ID, &map[string]interface{}{},
	).Put(payload)
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != 200 {
		return newErrorStatusNotOK(request)
	}

	return nil
}

func (api *API) SetPagePermissions(
	page *PageInfo,
	operation RestrictionOperation,
	restrictions []Restriction,
) error {
	var result interface{}

	request, err := api.json.Res(
		"setContentPermissions", &result,
	).Post([]interface{}{
		page.ID,
		operation,
		restrictions,
	})
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != 200 {
		return newErrorStatusNotOK(request)
	}

	if success, ok := result.(bool); !ok || !success {
		return fmt.Errorf(
			"'true' response expected, but '%v' encountered",
			result,
		)
	}

	return nil
}

func newErrorStatusNotOK(request *gopencils.Resource) error {
	if request.Raw.StatusCode == 401 {
		return errors.New(
			"Confluence API returned unexpected status: 401 (Unauthorized)",
		)
	}

	if request.Raw.StatusCode == 404 {
		return errors.New(
			"Confluence API returned unexpected status: 404 (Not Found)",
		)
	}

	output, _ := ioutil.ReadAll(request.Raw.Body)
	defer request.Raw.Body.Close()

	return fmt.Errorf(
		"Confluence API returned unexpected status: %v, "+
			"output: %s",
		request.Raw.Status, output,
	)
}
