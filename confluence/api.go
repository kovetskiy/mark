package confluence

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/kovetskiy/gopencils"
	"github.com/kovetskiy/lorg"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

type User struct {
	AccountID string `json:"accountId,omitempty"`
	UserKey   string `json:"userKey,omitempty"`
	UserName  string `json:"username,omitempty"`
	UserType  string `json:"type,omitempty"`
}

type API struct {
	rest *gopencils.Resource

	// it's deprecated accordingly to Atlassian documentation,
	// but it's only way to set permissions
	json    *gopencils.Resource
	BaseURL string
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
type form struct {
	buffer io.Reader
	writer *multipart.Writer
}

type tracer struct {
	prefix string
}

func (tracer *tracer) Printf(format string, args ...interface{}) {
	log.Tracef(nil, tracer.prefix+" "+format, args...)
}

func NewAPI(baseURL string, username string, password string, insecureSkipVerify bool) *API {
	var auth *gopencils.BasicAuth
	if username != "" {
		auth = &gopencils.BasicAuth{
			Username: username,
			Password: password,
		}
	}

	var httpClient *http.Client
	if insecureSkipVerify {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}

	rest := gopencils.Api(baseURL+"/rest/api", auth, httpClient, 3) // set option for 3 retries on failure
	if username == "" {
		if rest.Headers == nil {
			rest.Headers = http.Header{}
		}
		rest.SetHeader("Authorization", fmt.Sprintf("Bearer %s", password))
	}

	json := gopencils.Api(baseURL+"/rpc/json-rpc/confluenceservice-v2", auth, httpClient, 3)

	if log.GetLevel() == lorg.LevelTrace {
		rest.Logger = &tracer{"rest:"}
		json.Logger = &tracer{"json-rpc:"}
	}

	return &API{
		rest:    rest,
		json:    json,
		BaseURL: strings.TrimSuffix(baseURL, "/"),
	}
}

func (api *API) FindRootPage(space string) (*PageInfo, error) {
	page, err := api.FindPage(space, ``, "page")
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

	request, err := api.rest.Res(
		"space/"+space, &SpaceInfo{},
	).Get(payload)
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode == http.StatusNotFound || request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return &request.Response.(*SpaceInfo).Homepage, nil
}

func (api *API) FindPage(
	space string,
	title string,
	pageType string,
) (*PageInfo, error) {
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

	request, err := api.rest.Res(
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

	if len(result.Results) == 0 {
		return nil, nil
	}

	page := &result.Results[0]
	// Populate the base URL from the response _links.base
	if result.Links.Base != "" {
		page.Links.Base = result.Links.Base
	}

	return page, nil
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

	resource := api.rest.Res(
		"content/"+pageID+"/child/attachment", &result,
	)

	resource.Payload = form.buffer
	oldHeaders := resource.Headers.Clone()
	resource.Headers = http.Header{}
	if resource.Api.BasicAuth == nil {
		resource.Headers.Set("Authorization", oldHeaders.Get("Authorization"))
	}

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

	resource := api.rest.Res(
		"content/"+pageID+"/child/attachment/"+attachID+"/data", &result,
	)

	resource.Payload = form.buffer
	oldHeaders := resource.Headers.Clone()
	resource.Headers = http.Header{}
	if resource.Api.BasicAuth == nil {
		resource.Headers.Set("Authorization", oldHeaders.Get("Authorization"))
	}

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
		return info, karma.Format(
			err,
			"unable to unmarshal JSON response as full response format: %s",
			string(result),
		)
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
		return info, karma.Format(
			err,
			"unable to unmarshal JSON response as short response format: %s",
			string(result),
		)
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
		return nil, karma.Format(
			err,
			"unable to create form file",
		)
	}

	_, err = io.Copy(content, reader)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to copy i/o between form-file and file",
		)
	}

	commentWriter, err := writer.CreateFormField("comment")
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to create form field for comment",
		)
	}

	_, err = commentWriter.Write([]byte(comment))
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to write comment in form-field",
		)
	}

	err = writer.Close()
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to close form-writer",
		)
	}

	return &form{
		buffer: payload,
		writer: writer,
	}, nil
}

func (api *API) GetAttachments(pageID string) ([]AttachmentInfo, error) {
	result := struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}{}

	payload := map[string]string{
		"expand": "version,container",
		"limit":  "1000",
	}

	request, err := api.rest.Res(
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

	return result.Results, nil
}

func (api *API) GetPageByID(pageID string) (*PageInfo, error) {
	request, err := api.rest.Res(
		"content/"+pageID, &PageInfo{},
	).Get(map[string]string{"expand": "ancestors,version"})
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*PageInfo), nil
}

func (api *API) CreatePage(
	space string,
	pageType string,
	parent *PageInfo,
	title string,
	body string,
) (*PageInfo, error) {
	payload := map[string]interface{}{
		"type":  pageType,
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
		"metadata": map[string]interface{}{
			"properties": map[string]interface{}{
				"editor": map[string]interface{}{
					"value": "v2",
				},
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

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*PageInfo), nil
}

func (api *API) UpdatePage(page *PageInfo, newContent string, minorEdit bool, versionMessage string, newLabels []string, appearance string, emojiString string) error {
	nextPageVersion := page.Version.Number + 1
	oldAncestors := []map[string]interface{}{}

	if page.Type != "blogpost" && len(page.Ancestors) > 0 {
		// picking only the last one, which is required by confluence
		oldAncestors = []map[string]interface{}{
			{"id": page.Ancestors[len(page.Ancestors)-1].ID},
		}
	}

	properties := map[string]interface{}{
		// Fix to set full-width as has changed on Confluence APIs again.
		// https://jira.atlassian.com/browse/CONFCLOUD-65447
		//
		"content-appearance-published": map[string]interface{}{
			"value": appearance,
		},
		// content-appearance-draft should not be set as this is impacted by
		// the user editor default configurations - which caused the sporadic published widths.
	}

	if emojiString != "" {
		r, _ := utf8.DecodeRuneInString(emojiString)
		unicodeHex := fmt.Sprintf("%x", r)

		properties["emoji-title-draft"] = map[string]interface{}{
			"value": unicodeHex,
		}
		properties["emoji-title-published"] = map[string]interface{}{
			"value": unicodeHex,
		}
	}

	payload := map[string]interface{}{
		"id":    page.ID,
		"type":  page.Type,
		"title": page.Title,
		"version": map[string]interface{}{
			"number":    nextPageVersion,
			"minorEdit": minorEdit,
			"message":   versionMessage,
		},
		"ancestors": oldAncestors,
		"body": map[string]interface{}{
			"storage": map[string]interface{}{
				"value":          newContent,
				"representation": "storage",
			},
		},
		"metadata": map[string]interface{}{
			"properties": properties,
		},
	}

	request, err := api.rest.Res(
		"content/"+page.ID, &map[string]interface{}{},
	).Put(payload)
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return newErrorStatusNotOK(request)
	}

	return nil
}

func (api *API) AddPageLabels(page *PageInfo, newLabels []string) (*LabelInfo, error) {

	labels := []map[string]interface{}{}
	for _, label := range newLabels {
		if label != "" {
			item := map[string]interface{}{
				"prefix": "global",
				"name":   label,
			}
			labels = append(labels, item)
		}
	}

	payload := labels

	request, err := api.rest.Res(
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

	request, err := api.rest.Res(
		"content/"+page.ID+"/label", &LabelInfo{},
	).SetQuery(map[string]string{"name": label}).Delete()
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK && request.Raw.StatusCode != http.StatusNoContent {
		return nil, newErrorStatusNotOK(request)
	}

	return request.Response.(*LabelInfo), nil
}

func (api *API) GetPageLabels(page *PageInfo, prefix string) (*LabelInfo, error) {

	request, err := api.rest.Res(
		"content/"+page.ID+"/label", &LabelInfo{},
	).Get(map[string]string{"prefix": prefix})
	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return nil, newErrorStatusNotOK(request)
	}
	return request.Response.(*LabelInfo), nil
}

func (api *API) GetUserByName(name string) (*User, error) {
	var response struct {
		Results []struct {
			User User
		}
	}

	// Try the new path first
	_, err := api.rest.
		Res("search").
		Res("user", &response).
		Get(map[string]string{
			"cql": fmt.Sprintf("user.fullname~%q", name),
		})
	if err != nil {
		return nil, err
	}

	// Try old path
	if len(response.Results) == 0 {
		_, err := api.rest.
			Res("search", &response).
			Get(map[string]string{
				"cql": fmt.Sprintf("user.fullname~%q", name),
			})
		if err != nil {
			return nil, err
		}
	}

	if len(response.Results) == 0 {

		return nil, karma.
			Describe("name", name).
			Reason(
				"user with given name is not found",
			)
	}

	return &response.Results[0].User, nil
}

func (api *API) GetCurrentUser() (*User, error) {
	var user User

	_, err := api.rest.
		Res("user").
		Res("current", &user).
		Get()
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (api *API) RestrictPageUpdatesCloud(
	page *PageInfo,
	allowedUser string,
) error {
	user, err := api.GetCurrentUser()
	if err != nil {
		return err
	}

	var result interface{}

	request, err := api.rest.
		Res("content").
		Id(page.ID).
		Res("restriction", &result).
		Post([]map[string]interface{}{
			{
				"operation": "update",
				"restrictions": map[string]interface{}{
					"user": []map[string]interface{}{
						{
							"type":      "known",
							"accountId": user.AccountID,
						},
					},
				},
			},
		})
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != http.StatusOK {
		return newErrorStatusNotOK(request)
	}

	return nil
}

func (api *API) RestrictPageUpdatesServer(
	page *PageInfo,
	allowedUser string,
) error {
	var (
		err    error
		result interface{}
	)

	user, err := api.GetCurrentUser()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("content/%s/restriction", page.ID)
	data := []interface{}{
		map[string]interface{}{
			"operation": "update",
			"restrictions": map[string]interface{}{
				"user": []map[string]interface{}{
					{
						"type":     user.UserType,
						"username": user.UserName,
					},
				},
			},
		},
	}
	req := api.rest.Res(
		url, &result,
	)
	request, err := req.Put(data)
	if err != nil {
		return err
	}
	/*
	   [
	       {
	           "operation": "update",
	           "restrictions": {
	               "user": [
	                   {
	                       "type": "known",
	                       "username": "username here"
	                   }
	               ]
	           }
	       }
	   ]
	*/

	if request.Raw.StatusCode != http.StatusOK {
		return newErrorStatusNotOK(request)
	}

	/*
		Don't know how to adjust this. The response is
		-----
			{
			"_links": {
				"base": "CONFLUENCE-URL",
				"byOperation": "CONFLUENCE-URL/rest/api/content/308576838/restriction/byOperation",
				"context": ""
			},
			"limit": 200,
			"restrictionsHash": "293b4be2a692fca6e35218ce22b48492835524aa078df302a7681d78dab9074e",
			"results": [
				{
				"_expandable": {
					"content": "/rest/api/content/308576838"
				},
				"_links": {
					"self": "CONFLUENCE-URL/rest/api/content/308576838/restriction/byOperation/read"
				},
				"operation": "read",
				"restrictions": {
					"group": {
					"limit": 200,
					"results": [],
					"size": 0,
					"start": 0
					},
					"user": {
					"limit": 200,
					"results": [],
					"size": 0,
					"start": 0
					}
				}
				},
				{
				"_expandable": {
					"content": "/rest/api/content/308576838"
				},
				"_links": {
					"self": "CONFLUENCE-URL/rest/api/content/308576838/restriction/byOperation/update"
				},
				"lastModificationDate": "2025-08-01T15:27:01.163+02:00",
				"operation": "update",
				"restrictions": {
					"group": {
					"limit": 200,
					"results": [],
					"size": 0,
					"start": 0
					},
					"user": {
					"limit": 200,
					"results": [
						{
						"_expandable": {
							"status": ""
						},
						"_links": {
							"self": "CONFLUENCE-URL/rest/api/user?key=USER-KEY"
						},
						"displayName": "confluence-csa-automation",
						"profilePicture": {
							"height": 48,
							"isDefault": false,
							"path": "/download/attachments/155080056/user-avatar",
							"width": 48
						},
						"type": "known",
						"userKey": "USER-KEY",
						"username": "confluence-csa-au"
						}
					],
					"size": 1,
					"start": 0
					}
				}
				}
			],
			"size": 2,
			"start": 0
			}
		-----
		if success, ok := result.(bool); !ok || !success {
			return fmt.Errorf(
				"'true' response expected, but '%v' encountered",
				result,
			)
		}
	*/

	return nil
}

func (api *API) RestrictPageUpdates(
	page *PageInfo,
	allowedUser string,
) error {
	var err error

	if strings.HasSuffix(api.rest.Api.BaseUrl.Host, "jira.com") || strings.HasSuffix(api.rest.Api.BaseUrl.Host, "atlassian.net") {
		err = api.RestrictPageUpdatesCloud(page, allowedUser)
	} else {
		err = api.RestrictPageUpdatesServer(page, allowedUser)
	}

	return err
}

func newErrorStatusNotOK(request *gopencils.Resource) error {
	if request.Raw.StatusCode == http.StatusUnauthorized {
		return errors.New(
			"the Confluence API returned unexpected status: 401 (Unauthorized)",
		)
	}

	if request.Raw.StatusCode == http.StatusNotFound {
		return errors.New(
			"the Confluence API returned unexpected status: 404 (Not Found)",
		)
	}

	output, _ := io.ReadAll(request.Raw.Body)
	defer func() {
		_ = request.Raw.Body.Close()
	}()

	return fmt.Errorf(
		"the Confluence API returned unexpected status: %v, "+
			"output: %q",
		request.Raw.Status, output,
	)
}
