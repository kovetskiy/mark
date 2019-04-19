package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/kovetskiy/godocs"
	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/kovetskiy/mark/pkg/mark"
	"github.com/reconquest/cog"
	"github.com/reconquest/karma-go"
	"github.com/zazab/zhash"
)

const (
	usage = `mark - tool for updating Atlassian Confluence pages from markdown.

This is very usable if you store documentation to your orthodox software in git
repository and don't want to do a handjob with updating Confluence page using
fucking tinymce wysiwyg enterprise core editor.

You can store a user credentials in the configuration file, which should be
located in ~/.config/mark with following format:
  username = "smith"
  password = "matrixishere"
  base_url = "http://confluence.local"
where 'smith' it's your username, 'matrixishere' it's your password and
'http://confluence.local' is base URL for your Confluence instance.

Mark understands extended file format, which, still being valid markdown,
contains several metadata headers, which can be used to locate page inside
Confluence instance and update it accordingly.

File in extended format should follow specification:

  []:# (Space: <space key>)
  []:# (Parent: <parent 1>)
  []:# (Parent: <parent 2>)
  []:# (Title: <title>)

  <page contents>

There can be any number of 'Parent' headers, if mark can't find specified
parent by title, it will be created.

Also, optional following headers are supported:

  * []:# (Layout: <article|plain>)

    - (default) article: content will be put in narrow column for ease of
      reading;
    - plain: content will fill all page;

Usage:
  mark [options] [-u <username>] [-p <password>] [-k] [-l <url>] -f <file>
  mark [options] [-u <username>] [-p <password>] [-k] [-b <url>] -f <file>
  mark [options] [-u <username>] [-p <password>] [-k] [-n] -c <file>
  mark -v | --version
  mark -h | --help

Options:
  -u <username>        Use specified username for updating Confluence page.
  -p <password>        Use specified password for updating Confluence page.
  -l <url>             Edit specified Confluence page.
                        If -l is not specified, file should contain metadata (see
                        above).
  -b --base-url <url>  Base URL for Confluence.
                        Alternative option for base_url config field.
  -f <file>            Use specified markdown file for converting to html.
  -k                   Lock page editing to current user only to prevent accidental
                        manual edits over Confluence Web UI.
  --dry-run            Show resulting HTML and don't update Confluence page content.
  --trace              Enable trace logs.
  -h --help            Show this screen and call 911.
  -v --version         Show version.
`
)

var (
	log *cog.Logger
)

func initlog(trace bool) {
	stderr := lorg.NewLog()
	stderr.SetIndentLines(true)
	stderr.SetFormat(
		lorg.NewFormat("${time} ${level:[%s]:right:short} ${prefix}%s"),
	)

	log = cog.NewLogger(stderr)

	if trace {
		log.SetLevel(lorg.LevelTrace)
	}
}

func main() {
	args, err := godocs.Parse(usage, "mark 1.0", godocs.UsePager)
	if err != nil {
		panic(err)
	}

	var (
		targetFile, _ = args["-f"].(string)
		dryRun        = args["--dry-run"].(bool)
		editLock      = args["-k"].(bool)
		trace         = args["--trace"].(bool)
	)

	initlog(trace)

	config, err := getConfig(filepath.Join(os.Getenv("HOME"), ".config/mark"))
	if err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}

	markdownData, err := ioutil.ReadFile(targetFile)
	if err != nil {
		log.Fatal(err)
	}

	meta, err := mark.ExtractMeta(markdownData)
	if err != nil {
		log.Fatal(err)
	}

	htmlData := mark.CompileMarkdown(markdownData)

	if dryRun {
		fmt.Println(string(htmlData))
		os.Exit(0)
	}

	creds, err := GetCredentials(args, config)
	if err != nil {
		log.Fatal(err)
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	if creds.PageID != "" && meta != nil {
		log.Warningf(
			nil,
			`specified file contains metadata, `+
				`but it will be ignored due specified command line URL`,
		)

		meta = nil
	}

	if creds.PageID == "" && meta == nil {
		log.Fatalf(
			nil,
			`specified file doesn't contain metadata `+
				`and URL is not specified via command line `+
				`or doesn't contain pageId GET-parameter`,
		)
	}

	var target *confluence.PageInfo

	if meta != nil {
		page, err := resolvePage(api, meta)
		if err != nil {
			log.Fatalf(
				karma.Describe("title", meta.Title).Reason(err),
				"unable to resolve page",
			)
		}

		target = page
	} else {
		if creds.PageID == "" {
			log.Fatalf(nil, "URL should provide 'pageId' GET-parameter")
		}

		page, err := api.GetPageByID(creds.PageID)
		if err != nil {
			log.Fatalf(err, "unable to retrieve page by id")
		}

		target = page
	}

	mark.ResolveAttachments(api, target, ".", meta.Attachments)

	err = api.UpdatePage(
		target,
		MacroLayout{meta.Layout, [][]byte{htmlData}}.Render(),
	)
	if err != nil {
		log.Fatal(err)
	}

	if editLock {
		log.Infof(
			nil,
			`edit locked on page %q by user %q to prevent manual edits`,
			target.Title,
			creds.Username,
		)

		err := api.SetPagePermissions(
			target,
			confluence.RestrictionEdit,
			[]confluence.Restriction{
				{User: creds.Username},
			},
		)
		if err != nil {
			log.Fatal(err)
		}
	}

	fmt.Printf(
		"page successfully updated: %s\n",
		creds.BaseURL+target.Links.Full,
	)

}

func resolvePage(
	api *confluence.API,
	meta *mark.Meta,
) (*confluence.PageInfo, error) {
	page, err := api.FindPage(meta.Space, meta.Title)
	if err != nil {
		return nil, karma.Format(
			err,
			"error during finding page %q",
			meta.Title,
		)
	}

	ancestry := meta.Parents
	if page != nil {
		ancestry = append(ancestry, page.Title)
	}

	if len(ancestry) > 0 {
		page, err := mark.ValidateAncestry(
			api,
			meta.Space,
			ancestry,
		)
		if err != nil {
			return nil, err
		}

		if page == nil {
			log.Warningf(
				nil,
				"page %q is not found ",
				meta.Parents[len(ancestry)-1],
			)
		}

		path := meta.Parents
		path = append(path, meta.Title)

		log.Debugf(
			nil,
			"resolving page path: ??? > %s",
			strings.Join(path, ` > `),
		)
	}

	parent, err := mark.EnsureAncestry(
		api,
		meta.Space,
		meta.Parents,
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"can't create ancestry tree: %s",
			strings.Join(meta.Parents, ` > `),
		)
	}

	titles := []string{}
	for _, page := range parent.Ancestors {
		titles = append(titles, page.Title)
	}

	titles = append(titles, parent.Title)

	log.Infof(
		nil,
		"page will be stored under path: %s > %s",
		strings.Join(titles, ` > `),
		meta.Title,
	)

	if page == nil {
		page, err := api.CreatePage(meta.Space, parent, meta.Title, ``)
		if err != nil {
			return nil, karma.Format(
				err,
				"can't create page %q",
				meta.Title,
			)
		}

		return page, nil
	}

	return page, nil
}

func getConfig(path string) (zhash.Hash, error) {
	configData := map[string]interface{}{}
	_, err := toml.DecodeFile(path, &configData)
	if err != nil {
		if os.IsNotExist(err) {
			return zhash.NewHash(), err
		}

		return zhash.NewHash(), karma.Format(
			err,
			"can't decode toml file: %s",
			path,
		)
	}

	return zhash.HashFromMap(configData), nil
}
