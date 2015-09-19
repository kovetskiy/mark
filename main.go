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

Mark it's tool for syncing your markdown files with Atlassian Confluence pages.

This is very usable if you store documentation to your orthodox software in git
repository and don't want to do a handjob with updating Confluence page using
fucking tinymce wysiwyg enterprise core editor.

You can store a user credentials in the configuration file, which should be
located in ~/.config/mark with following format:
    username = "smith"
    password = "matrixishere"
where 'smith' it's your username, and 'matrixishere' it's your password.

Mark can read Confluence page URL and markdown file path from another specified
configuration file, which you can specify using -c <file> flag. It is very
usable for git hooks. That file should have following format:
    url = "http://confluence.local/pages/viewpage.action?pageId=123456"
    file = "docs/README.md"

Usage:
    mark [--dry-run] [-u <username>] [-p <password>] -l <url> -f <file>
    mark [--dry-run] [-u <username>] [-p <password>] -c <file>
    mark -v | --version
	mark -h | --help

Options:
    -u <username>  Use specified username for updating Confluence page.
    -p <password>  Use specified password for updating Confluence page.
    -l <url>       Edit specified Confluence page.
    -f <file>      Use specified markdown file for converting to html.
    -c <file>      Specify configuration file which should be used for reading
                   Confluence page URL and markdown file path.
	--dry-run      Show resulting HTML and don't update Confluence page content.
    -h --help      Show this screen and call 911.
    -v --version   Show version.
`
)

type PageInfo struct {
	Title   string `json:"title"`
	Version struct {
		Number int64 `json:"number"`
	} `json:"version"`
}

func main() {
	args, err := docopt.Parse(usage, nil, true, "mark 1.0", false, true)
	if err != nil {
		panic(err)
	}

	var (
		username, _   = args["-u"].(string)
		password, _   = args["-p"].(string)
		targetURL, _  = args["-l"].(string)
		targetFile, _ = args["-f"].(string)
		dryRun        = args["--dry-run"].(bool)

		optionsFile, shouldReadOptions = args["-c"].(string)
	)

	config, err := getConfig(filepath.Join(os.Getenv("HOME"), ".config/mark"))
	if err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}

	if shouldReadOptions {
		optionsConfig, err := getConfig(optionsFile)
		if err != nil {
			log.Fatalf("can't read options config '%s': %s", optionsFile, err)
		}

		targetURL, err = optionsConfig.GetString("url")
		if err != nil {
			log.Fatal(
				"can't read `url` value from options file (%s): %s",
				optionsFile, err,
			)
		}

		targetFile, err = optionsConfig.GetString("file")
		if err != nil {
			log.Fatal(
				"can't read `file` value from options file (%s): %s",
				optionsFile, err,
			)
		}
	}

	markdownData, err := ioutil.ReadFile(targetFile)
	if err != nil {
		log.Fatal(err)
	}

	htmlData := blackfriday.MarkdownCommon(markdownData)

	if dryRun {
		fmt.Println(string(htmlData))
		os.Exit(0)
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

			log.Fatalf("can't read username configuration variable: %s", err)
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

			log.Fatalf("can't read password configuration variable: %s", err)
		}
	}

	url, err := url.Parse(targetURL)
	if err != nil {
		log.Fatal(err)
	}

	api := gopencils.Api(
		"http://"+url.Host+"/rest/api",
		&gopencils.BasicAuth{username, password},
	)

	pageID := url.Query().Get("pageId")
	if pageID == "" {
		log.Fatalf("URL should contains 'pageId' parameter")
	}

	pageInfo, err := getPageInfo(api, pageID)
	if err != nil {
		log.Fatal(err)
	}

	err = updatePage(api, pageID, pageInfo, htmlData)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("page %s successfully updated\n", targetURL)
}

func updatePage(
	api *gopencils.Resource, pageID string,
	pageInfo PageInfo, newContent []byte,
) error {
	nextPageVersion := pageInfo.Version.Number + 1

	payload := map[string]interface{}{
		"id":    pageID,
		"type":  "page",
		"title": pageInfo.Title,
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
		output, _ := ioutil.ReadAll(request.Raw.Body)
		defer request.Raw.Body.Close()

		return fmt.Errorf(
			"Confluence REST API returns unexpected HTTP status: %s, "+
				"output: %s",
			request.Raw.Status, output,
		)
	}

	return nil
}

func getPageInfo(
	api *gopencils.Resource, pageID string,
) (PageInfo, error) {
	request, err := api.Res("content/"+pageID, &PageInfo{}).Get()
	if err != nil {
		return PageInfo{}, err
	}

	if request.Raw.StatusCode == 401 {
		return PageInfo{}, fmt.Errorf("authentification failed")
	}

	if request.Raw.StatusCode == 404 {
		return PageInfo{}, fmt.Errorf(
			"page with id '%s' not found, Confluence REST API returns 404",
			pageID,
		)
	}

	if request.Raw.StatusCode != 200 {
		return PageInfo{}, fmt.Errorf(
			"Confluence REST API returns unexpected HTTP status: %s",
			request.Raw.Status,
		)
	}

	response := request.Response.(*PageInfo)

	return *response, nil
}

func getConfig(path string) (zhash.Hash, error) {
	configData := map[string]interface{}{}
	_, err := toml.DecodeFile(path, &configData)
	if err != nil {
		if os.IsNotExist(err) {
			return zhash.NewHash(), err
		}

		return zhash.NewHash(), fmt.Errorf("can't decode toml file: %s", err)
	}

	return zhash.HashFromMap(configData), nil
}
