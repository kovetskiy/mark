package main

import (
	"fmt"
	"bytes"
	"io/ioutil"
	"path/filepath"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"github.com/bndr/gopencils"
)

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

	download *gopencils.Resource

	// it's deprecated accordingly to Atlassian documentation,
	// but it's only way to set permissions
	json *gopencils.Resource
}

func NewAPI(baseURL string, username string, password string) *API {
	auth := &gopencils.BasicAuth{username, password}

	return &API{
		rest: gopencils.Api(baseURL+"/rest/api", auth),
		download: gopencils.Api(baseURL, auth),

		json: gopencils.Api(
			baseURL+"/rpc/json-rpc/confluenceservice-v2",
			auth,
		),
	}
}

func (api *API) findRootPage(space string) (*PageInfo, error) {
	page, err := api.findPage(space, ``)
	if err != nil {
		return nil, fmt.Errorf(
			`can't obtain first page from space '%s': %s`,
			space,
			err,
		)
	}

	if len(page.Ancestors) == 0 {
		return nil, fmt.Errorf(
			"page '%s' from space '%s' has no parents",
			page.Title,
			space,
		)
	}

	return &PageInfo{
		ID:    page.Ancestors[0].Id,
		Title: page.Ancestors[0].Title,
	}, nil
}

func (api *API) findPage(space string, title string) (*PageInfo, error) {

	result := struct {
		Results []PageInfo `json:"results"`
	}{}

	payload := map[string]string{
		"spaceKey": space,
		"expand":   "ancestors,version,body.storage,title",
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

	if request.Raw.StatusCode == 401 {
		return nil, fmt.Errorf("authentication failed")
	}

	if request.Raw.StatusCode != 200 {
		return nil, fmt.Errorf(
			"Confluence REST API returns unexpected non-200 HTTP status: %s",
			request.Raw.Status,
		)
	}

	if len(result.Results) == 0 {
		return nil, nil
	}

	return &result.Results[0], nil
}

func (api *API) getPageByID(pageID string) (*PageInfo, error) {
	request, err := api.rest.Res(
		"content/"+pageID, &PageInfo{},
	).Get(map[string]string{"expand": "ancestors,version"})

	if err != nil {
		return nil, err
	}

	if request.Raw.StatusCode == 401 {
		return nil, fmt.Errorf("authentication failed")
	}

	if request.Raw.StatusCode == 404 {
		return nil, fmt.Errorf(
			"page with id '%s' not found, Confluence REST API returns 404",
			pageID,
		)
	}

	if request.Raw.StatusCode != 200 {
		return nil, fmt.Errorf(
			"Confluence REST API returns unexpected HTTP status: %s",
			request.Raw.Status,
		)
	}

	return request.Response.(*PageInfo), nil
}

func (api *API) createPage(
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
		output, _ := ioutil.ReadAll(request.Raw.Body)
		defer request.Raw.Body.Close()

		return nil, fmt.Errorf(
			"Confluence REST API returns unexpected non-200 HTTP status: %s, "+
				"output: %s",
			request.Raw.Status, output,
		)
	}

	return request.Response.(*PageInfo), nil
}

func (api *API) updatePage(
	page *PageInfo, newContent string,
) error {
	nextPageVersion := page.Version.Number + 1

	if len(page.Ancestors) == 0 {
		return fmt.Errorf(
			"page '%s' info does not contain any information about parents",
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
		output, _ := ioutil.ReadAll(request.Raw.Body)
		defer request.Raw.Body.Close()

		return fmt.Errorf(
			"Confluence REST API returns unexpected non-200 HTTP status: %s, "+
				"output: %s",
			request.Raw.Status, output,
		)
	}

	return nil
}

func (api *API) setPagePermissions(
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
		output, _ := ioutil.ReadAll(request.Raw.Body)
		defer request.Raw.Body.Close()

		return fmt.Errorf(
			"Confluence JSON RPC returns unexpected non-200 HTTP status: %s, "+
				"output: %s",
			request.Raw.Status, output,
		)
	}

	if success, ok := result.(bool); !ok || !success {
		return fmt.Errorf(
			"'true' response expected, but '%v' encountered",
			result,
		)
	}

	return nil
}

func (api *API) ensureAttachment(
	pageId string,
	path string,
	title string,
	fileData []byte,
	currentAttachment *AttachmentInfo,
) error {

	filename := filepath.Base(path)
	extension := filepath.Ext(path)
 
	var err error
	if fileData == nil {
		fileData, err = ioutil.ReadFile(path)
		if err != nil {
			return err
		}
	}
 

	// any changes

	if title == "" {
		title = filename[0:len(filename)-len(extension)]
	}

	url := "content/"+pageId+"/child/attachment"
	
	if (currentAttachment != nil) {
		if currentAttachment.Data  != nil && bytes.Compare(currentAttachment.Data, fileData) == 0 {
			logger.Debug("Content of file has not changed ")

			return nil

			/* does not work...
			logger.Debug("%#v / %#v", title, currentAttachment.Title)

			if (title == currentAttachment.Title) {
				logger.Debug("Title of file has not changed")
				return nil
			}
			*/
		}

		url += "/" + currentAttachment.ID+ "/data"
	}
	
	res :=  api.rest.Res(url , nil)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename ))

	if extension == ".png" {
		h.Set("Content-Type", "image/png")
	} else  if extension == ".jpg" || extension == ".jepg" {
		h.Set("Content-Type", "image/jepg")
	} else  {
		h.Set("Content-Type", "image/*")
	}
	part, err :=  writer.CreatePart(h)

	if err != nil {
		return err
	}

	part.Write(fileData)

	writer.WriteField("comment", "imported from " + path)
	writer.WriteField("title", title)
	writer.WriteField("minorEdit","true");
	err = writer.Close()
	if err != nil {
		return  err
	}

	res.Payload =  body;
	res.Headers.Add("Content-Type", writer.FormDataContentType())
	res.Headers.Add("X-Atlassian-Token","no-check");

	request, err := res.Post()
	
	logger.Tracef("Add attachment: \n%#v \n%#v (%#v)",res, request.Raw, err)

	if err != nil {
		return err
	}

	if request.Raw.StatusCode == 401 {
		return fmt.Errorf("authentication failed")
	}

	if request.Raw.StatusCode != 200 {
		return fmt.Errorf(
			"Confluence REST API returns unexpected non-200 HTTP status: %s",
			request.Raw.Status,
		)
	}
	
	return nil
}




func (api *API) getAttachment(
	pageId string,
	path string,
) (*AttachmentInfo, error) {
 
	result := struct {
		Results []AttachmentInfo `json:"results"`
	}{}

 	payload := map[string]string{
		 "filename": filepath.Base(path),
		 "expand": "title",
	 }


	request, err := api.rest.Res(
		"content/"+pageId+"/child/attachment", &result,
	).Get(payload)

	if err != nil {
		return  nil, err
	}

	

	if request.Raw.StatusCode == 401 {
		return nil, fmt.Errorf("authentication failed")
	}

	if request.Raw.StatusCode != 200 {
		return nil, fmt.Errorf(
			"Confluence REST API returns unexpected non-200 HTTP status: %s",
			request.Raw.Status,
		)
	}
	
	if len(result.Results) == 0 {
		return nil, nil
	}

	attachmentInfo := &result.Results[0]

	attachmentInfo.Data, err = api.downloadImage(attachmentInfo.Links.Download)

	if err != nil {
		return  nil, err
	}


 
	return attachmentInfo, nil
}

func (api *API) downloadImage(
	 downloadPath string,
) ([]byte, error) {
 
	url := api.download.Api.BaseUrl.String() + downloadPath
	

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if api.download.Api.BasicAuth != nil {
		req.SetBasicAuth(api.download.Api.BasicAuth.Username, api.download.Api.BasicAuth.Password)
	}
 
	resp, err := api.download.Api.Client.Do(req)
	
	if err != nil {
		return nil, err
	}
 
	if resp.StatusCode >= 400 {
		return nil, nil
	}
 
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err  != nil {
		return nil, err
	}
	return body, nil
}