package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/kovetskiy/godocs"
	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/kovetskiy/mark/pkg/mark"
	"github.com/reconquest/colorgful"
	"github.com/reconquest/karma-go"
	"github.com/russross/blackfriday"
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

Mark can read Confluence page URL and markdown file path from another specified
configuration file, which you can specify using -c <file> flag. It is very
usable for git hooks. That file should have following format:
  url = "http://confluence.local/pages/viewpage.action?pageId=123456"
  file = "docs/README.md"

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
	logger = lorg.NewLog()
)

func initLogger(trace bool) {
	if trace {
		logger.SetLevel(lorg.LevelTrace)
	}

	logFormat := `${time} ${level:[%s]:right:true} %s`

	if format := os.Getenv("LOG_FORMAT"); format != "" {
		logFormat = format
	}

	logger.SetFormat(colorgful.MustApplyDefaultTheme(
		logFormat,
		colorgful.Default,
	))
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

	initLogger(trace)

	config, err := getConfig(filepath.Join(os.Getenv("HOME"), ".config/mark"))
	if err != nil && !os.IsNotExist(err) {
		logger.Fatal(err)
	}

	markdownData, err := ioutil.ReadFile(targetFile)
	if err != nil {
		logger.Fatal(err)
	}

	meta, err := mark.ExtractMeta(markdownData)
	if err != nil {
		logger.Fatal(err)
	}

	htmlData := compileMarkdown(markdownData)

	if dryRun {
		fmt.Println(string(htmlData))
		os.Exit(0)
	}

	creds, err := GetCredentials(args, config)
	if err != nil {
		logger.Fatal(err)
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	if creds.PageID != "" && meta != nil {
		logger.Warningf(
			`specified file contains metadata, ` +
				`but it will be ignored due specified command line URL`,
		)

		meta = nil
	}

	if creds.PageID == "" && meta == nil {
		logger.Fatalf(
			`specified file doesn't contain metadata ` +
				`and URL is not specified via command line ` +
				`or doesn't contain pageId GET-parameter`,
		)
	}

	var target *confluence.PageInfo

	if meta != nil {
		page, err := resolvePage(api, meta)
		if err != nil {
			logger.Fatal(err)
		}

		target = page
	} else {
		if creds.PageID == "" {
			logger.Fatalf("URL should provide 'pageId' GET-parameter")
		}

		page, err := api.GetPageByID(creds.PageID)
		if err != nil {
			logger.Fatal(err)
		}

		target = page
	}

	err = api.UpdatePage(
		target,
		MacroLayout{meta.Layout, [][]byte{htmlData}}.Render(),
	)
	if err != nil {
		logger.Fatal(err)
	}

	if editLock {
		logger.Infof(
			`edit locked on page '%s' by user '%s' to prevent manual edits`,
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
			logger.Fatal(err)
		}
	}

	fmt.Printf(
		"page successfully updated: %s\n",
		creds.BaseURL+target.Links.Full,
	)

}

// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because blackfriday markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> for whatever reason.
func compileMarkdown(markdown []byte) []byte {
	colon := regexp.MustCompile(`---BLACKFRIDAY-COLON---`)

	tags := regexp.MustCompile(`<(/?\S+):(\S+)>`)

	markdown = tags.ReplaceAll(
		markdown,
		[]byte(`<$1`+colon.String()+`$2>`),
	)

	renderer := ConfluenceRenderer{
		blackfriday.HtmlRenderer(
			blackfriday.HTML_USE_XHTML|
				blackfriday.HTML_USE_SMARTYPANTS|
				blackfriday.HTML_SMARTYPANTS_FRACTIONS|
				blackfriday.HTML_SMARTYPANTS_DASHES|
				blackfriday.HTML_SMARTYPANTS_LATEX_DASHES,
			"", "",
		),
	}

	html := blackfriday.MarkdownOptions(
		markdown,
		renderer,
		blackfriday.Options{
			Extensions: blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
				blackfriday.EXTENSION_TABLES |
				blackfriday.EXTENSION_FENCED_CODE |
				blackfriday.EXTENSION_AUTOLINK |
				blackfriday.EXTENSION_STRIKETHROUGH |
				blackfriday.EXTENSION_SPACE_HEADERS |
				blackfriday.EXTENSION_HEADER_IDS |
				blackfriday.EXTENSION_BACKSLASH_LINE_BREAK |
				blackfriday.EXTENSION_DEFINITION_LISTS,
		},
	)

	html = colon.ReplaceAll(html, []byte(`:`))

	return html
}

func resolvePage(
	api *confluence.API,
	meta *mark.Meta,
) (*confluence.PageInfo, error) {
	page, err := api.FindPage(meta.Space, meta.Title)
	if err != nil {
		return nil, karma.Format(
			err,
			"error during finding page '%s': %s",
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
			logger.Warningf(
				"page '%s' is not found ",
				meta.Parents[len(ancestry)-1],
			)
		}

		path := meta.Parents
		path = append(path, meta.Title)

		logger.Debugf(
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
			"can't create ancestry tree: %s; error: %s",
			strings.Join(meta.Parents, ` > `),
		)
	}

	titles := []string{}
	for _, page := range parent.Ancestors {
		titles = append(titles, page.Title)
	}

	titles = append(titles, parent.Title)

	logger.Infof(
		"page will be stored under path: %s > %s",
		strings.Join(titles, ` > `),
		meta.Title,
	)

	if page == nil {
		page, err := api.CreatePage(meta.Space, parent, meta.Title, ``)
		if err != nil {
			return nil, karma.Format(
				err,
				"can't create page '%s': %s",
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
		)
	}

	return zhash.HashFromMap(configData), nil
}
