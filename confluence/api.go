package confluence

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/kovetskiy/lorg"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"resty.dev/v3"
)

type User struct {
	AccountID string `json:"accountId,omitempty"`
	UserKey   string `json:"userKey,omitempty"`
}

type API struct {
	rest *resty.Client

	// it's deprecated accordingly to Atlassian documentation,
	// but it's only way to set permissions
	json    *resty.Client
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
type tracer struct {
	prefix string
}

func (tracer *tracer) Errorf(format string, args ...interface{}) {
	log.Tracef(nil, tracer.prefix+" "+format, args...)
}

func (tracer *tracer) Warnf(format string, args ...interface{}) {
	log.Tracef(nil, tracer.prefix+" "+format, args...)
}

func (tracer *tracer) Debugf(format string, args ...interface{}) {
	log.Tracef(nil, tracer.prefix+" "+format, args...)
}

func NewAPI(baseURL string, username string, password string, insecureSkipVerify bool) *API {
	rest := resty.New().
		SetBaseURL(baseURL+"/rest/api").
		SetRetryCount(3)

	json := resty.New().
		SetBaseURL(baseURL+"/rpc/json-rpc/confluenceservice-v2").
		SetRetryCount(3)

	if username != "" {
		rest.SetBasicAuth(username, password)
		json.SetBasicAuth(username, password)
	} else {
		rest.SetAuthToken(password)
		json.SetAuthToken(password)
	}

	if insecureSkipVerify {
		tlsConfig := &tls.Config{InsecureSkipVerify: true}
		rest.SetTLSClientConfig(tlsConfig)
		json.SetTLSClientConfig(tlsConfig)
	}

	if log.GetLevel() == lorg.LevelTrace {
		rest.SetDebug(true)
		rest.SetLogger(&tracer{"rest:"})
		json.SetDebug(true)
		json.SetLogger(&tracer{"json-rpc:"})
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
	var result SpaceInfo
	resp, err := api.rest.R().
		SetQueryParams(map[string]string{
			"expand": "homepage",
		}).
		SetResult(&result).
		Get("space/" + space)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorResponseNotOK(resp)
	}

	return &result.Homepage, nil
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

	resp, err := api.rest.R().
		SetQueryParams(payload).
		SetResult(&result).
		Get("content/")
	if err != nil {
		return nil, err
	}

	// allow 404 because it's fine if page is not found,
	// the function will return nil, nil
	if resp.StatusCode() != http.StatusNotFound && resp.StatusCode() != http.StatusOK {
		return nil, newErrorResponseNotOK(resp)
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
	var result struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}

	resp, err := api.rest.R().
		SetFileReader("file", name, reader).
		SetFormData(map[string]string{
			"comment": comment,
		}).
		SetHeader("X-Atlassian-Token", "no-check").
		SetResult(&result).
		Post("content/" + pageID + "/child/attachment")
	if err != nil {
		return AttachmentInfo{}, err
	}

	if resp.StatusCode() != http.StatusOK {
		return AttachmentInfo{}, newErrorResponseNotOK(resp)
	}

	if len(result.Results) == 0 {
		return AttachmentInfo{}, errors.New(
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

	return result.Results[0], nil
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
	var result json.RawMessage

	resp, err := api.rest.R().
		SetFileReader("file", name, reader).
		SetFormData(map[string]string{
			"comment": comment,
		}).
		SetHeader("X-Atlassian-Token", "no-check").
		SetResult(&result).
		Post("content/" + pageID + "/child/attachment/" + attachID + "/data")
	if err != nil {
		return AttachmentInfo{}, err
	}

	if resp.StatusCode() != http.StatusOK {
		return AttachmentInfo{}, newErrorResponseNotOK(resp)
	}

	var extendedResponse struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}

	err = json.Unmarshal(result, &extendedResponse)
	if err != nil {
		return AttachmentInfo{}, karma.Format(
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

		return extendedResponse.Results[0], nil
	}

	var shortResponse AttachmentInfo
	err = json.Unmarshal(result, &shortResponse)
	if err != nil {
		return AttachmentInfo{}, karma.Format(
			err,
			"unable to unmarshal JSON response as short response format: %s",
			string(result),
		)
	}

	return shortResponse, nil
}

func (api *API) GetAttachments(pageID string) ([]AttachmentInfo, error) {
	result := struct {
		Links struct {
			Context string `json:"context"`
		} `json:"_links"`
		Results []AttachmentInfo `json:"results"`
	}{}

	resp, err := api.rest.R().
		SetQueryParams(map[string]string{
			"expand": "version,container",
			"limit":  "1000",
		}).
		SetResult(&result).
		Get("content/" + pageID + "/child/attachment")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorResponseNotOK(resp)
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
	var result PageInfo
	resp, err := api.rest.R().
		SetQueryParams(map[string]string{
			"expand": "ancestors,version",
		}).
		SetResult(&result).
		Get("content/" + pageID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorResponseNotOK(resp)
	}

	return &result, nil
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

	var result PageInfo
	resp, err := api.rest.R().
		SetBody(payload).
		SetResult(&result).
		Post("content/")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorResponseNotOK(resp)
	}

	return &result, nil
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

	resp, err := api.rest.R().
		SetBody(payload).
		Put("content/" + page.ID)
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return newErrorResponseNotOK(resp)
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

	var result LabelInfo
	resp, err := api.rest.R().
		SetBody(payload).
		SetResult(&result).
		Post("content/" + page.ID + "/label")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorResponseNotOK(resp)
	}

	return &result, nil
}

func (api *API) DeletePageLabel(page *PageInfo, label string) (*LabelInfo, error) {
	var result LabelInfo
	resp, err := api.rest.R().
		SetQueryParam("name", label).
		SetResult(&result).
		Delete("content/" + page.ID + "/label")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return nil, newErrorResponseNotOK(resp)
	}

	return &result, nil
}

func (api *API) GetPageLabels(page *PageInfo, prefix string) (*LabelInfo, error) {
	var result LabelInfo
	resp, err := api.rest.R().
		SetQueryParam("prefix", prefix).
		SetResult(&result).
		Get("content/" + page.ID + "/label")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, newErrorResponseNotOK(resp)
	}
	return &result, nil
}

func (api *API) GetUserByName(name string) (*User, error) {
	var response struct {
		Results []struct {
			User User
		}
	}

	// Try the new path first
	_, err := api.rest.R().
		SetQueryParam("cql", fmt.Sprintf("user.fullname~%q", name)).
		SetResult(&response).
		Get("search/user")
	if err != nil {
		return nil, err
	}

	// Try old path
	if len(response.Results) == 0 {
		_, err := api.rest.R().
			SetQueryParam("cql", fmt.Sprintf("user.fullname~%q", name)).
			SetResult(&response).
			Get("search")
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

	_, err := api.rest.R().
		SetResult(&user).
		Get("user/current")
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

	resp, err := api.rest.R().
		SetBody([]map[string]interface{}{
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
		}).
		SetResult(&result).
		Post("content/" + page.ID + "/restriction")
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return newErrorResponseNotOK(resp)
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

	resp, err := api.json.R().
		SetBody([]interface{}{
			page.ID,
			"Edit",
			[]map[string]interface{}{
				{
					"userName": allowedUser,
				},
			},
		}).
		SetResult(&result).
		Post("setContentPermissions")
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return newErrorResponseNotOK(resp)
	}

	if success, ok := result.(bool); !ok || !success {
		return fmt.Errorf(
			"'true' response expected, but '%v' encountered",
			result,
		)
	}

	return nil
}

func (api *API) RestrictPageUpdates(
	page *PageInfo,
	allowedUser string,
) error {
	var err error

	u, _ := url.Parse(api.rest.BaseURL())
	if strings.HasSuffix(u.Host, "jira.com") || strings.HasSuffix(u.Host, "atlassian.net") {
		err = api.RestrictPageUpdatesCloud(page, allowedUser)
	} else {
		err = api.RestrictPageUpdatesServer(page, allowedUser)
	}

	return err
}

func newErrorResponseNotOK(resp *resty.Response) error {
	if resp.StatusCode() == http.StatusUnauthorized {
		return errors.New(
			"the Confluence API returned unexpected status: 401 (Unauthorized)",
		)
	}

	if resp.StatusCode() == http.StatusNotFound {
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
