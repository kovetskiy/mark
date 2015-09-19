package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/bndr/gopencils"
	"github.com/docopt/docopt-go"
	"github.com/russross/blackfriday"
	"github.com/zazab/zhash"
)

const (
	usage = `Mark

You can store user credentials in configuration file, which should be located
in ~/.config/mark with following format:
	user = "smith"
	password = "matrixishere"

Usage:
	mark [-u <user>] [-p <pass>] -l <link> -f <file>

Options:
	-u <user>   Use specified username for updating Confluence page, this
					option can be specified using configuration file.
	-p <pass>   Use specified password for updagin Confluence page, this
					option can be specified using configuration file.
	-l <link>   Edit specified Confluence page.
	-f <file>   Use specified markdown file for converting to html.
`
)

type ResponseContent struct {
	Version struct {
		Number int64 `json:"number"`
	} `json:"version"`
}

func main() {
	args, err := docopt.Parse(usage, nil, true, "mark 1.0", true, true)
	if err != nil {
		panic(err)
	}

	var (
		username, _ = args["-u"].(string)
		password, _ = args["-p"].(string)
		targetLink  = args["-l"].(string)
		targetFile  = args["-f"].(string)
	)

	config, err := getConfig(filepath.Join(os.Getenv("HOME"), ".config/mark"))
	if err != nil {
		log.Fatal(err)
	}

	if username == "" {
		username, err = config.GetString("username")
		if err != nil {
			if zhash.IsNotFound(err) {
				log.Fatal(
					"Confluence username should be specified using -u " +
						"flag or be stored in configuration file",
				)
			}

			log.Fatal("can't read username configuration variable: %s", err)
		}
	}

	if password == "" {
		password, err = config.GetString("password")
		if err != nil {
			if zhash.IsNotFound(err) {
				log.Fatal(
					"Confluence password should be specified using -p " +
						"flag or be stored in configuration file",
				)
			}

			log.Fatal("can't read password configuration variable: %s", err)
		}
	}

	markdownData, err := ioutil.ReadFile(targetFile)
	if err != nil {
		log.Fatal(err)
	}

	htmlData := blackfriday.MarkdownCommon(markdownData)

	url, err := url.Parse(targetLink)
	if err != nil {
		log.Fatal(err)
	}

	api := gopencils.Api(
		"http://"+url.Host+"/rest/api",
		&gopencils.BasicAuth{username, password},
	)

	pageID := url.Query().Get("pageId")
	if pageID == "" {
		log.Fatal("URL should contains 'pageId' parameter")
	}

	version, err := getPageVersion(api, pageID)
	if err != nil {
		log.Fatal(err)
	}

	err = updatePage(api, pageID, version, htmlData)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("page %s successfully updated\n", targetLink)
}

func updatePage(
	api *gopencils.Resource, pageID string,
	currentPageVersion int64, newContent []byte,
) error {
	nextPageVersion := currentPageVersion + 1

	payload := map[string]interface{}{
		"id":   pageID,
		"type": "page",
		"version": map[string]interface{}{
			"number":    nextPageVersion,
			"minorEdit": false,
		},
		"body": map[string]interface{}{
			"storage": map[string]interface{}{
				"value":          string(newContent),
				"representation": "storage",
			},
		},
	}

	request, err := api.Res(
		"content/"+pageID, &map[string]interface{}{},
	).Put(payload)
	if err != nil {
		return err
	}

	if request.Raw.StatusCode != 200 {
		return fmt.Errorf(
			"Confluence REST API returns unexpected HTTP status: %s",
			request.Raw.Status,
		)
	}

	return nil
}

func getPageVersion(api *gopencils.Resource, pageID string) (int64, error) {
	request, err := api.Res("content/"+pageID, &ResponseContent{}).Get()
	if err != nil {
		return 0, err
	}

	if request.Raw.StatusCode == 401 {
		return 0, fmt.Errorf("authentification failed")
	}

	if request.Raw.StatusCode == 404 {
		return 0, fmt.Errorf(
			"page with id '%s' not found, Confluence REST API returns 404",
			pageID,
		)
	}

	if request.Raw.StatusCode != 200 {
		return 0, fmt.Errorf(
			"Confluence REST API returns unexpected HTTP status: %s",
			request.Raw.Status,
		)
	}

	response := request.Response.(*ResponseContent)

	return response.Version.Number, nil
}

func getConfig(path string) (zhash.Hash, error) {
	configData := map[string]interface{}{}
	_, err := toml.DecodeFile(path, &configData)
	if err != nil && !os.IsNotExist(err) {
		return zhash.NewHash(), fmt.Errorf("can't decode toml file: %s", err)
	}

	return zhash.HashFromMap(configData), nil
}
