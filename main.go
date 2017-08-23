package main

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
	"fmt"
	"os"
	"path/filepath"
	"encoding/xml"
	"io"
	"io/ioutil"
	"net/url"

	"github.com/BurntSushi/toml"
	"github.com/kovetskiy/godocs"
	"github.com/kovetskiy/lorg"
	"github.com/reconquest/colorgful"
	"github.com/reconquest/ser-go"
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

  <page s>

There can be any number of 'Parent' headers, if mark can't find specified
parent by title, it will be created.

Also, optional following headers are supported:

  * []:# (Layout: <article|plain>)

    - (default) article:  will be put in narrow column for ease of
      reading;
    - plain:  will fill all page;

Usage:
  mark [options] [-u <username>] [-p <password>] [-k] [-l <url>] -f <file>
  mark [options] [-u <username>] [-p <password>] [-k] [-n] -c <file>
  mark -v | --version
  mark -h | --help

Options:
  -u <username>  Use specified username for updating Confluence page.
  -p <password>  Use specified password for updating Confluence page.
  -l <url>       Edit specified Confluence page.
                  If -l is not specified, file should contain metadata (see
                  above).
  -f <file>      Use specified markdown file for converting to html.
  -c <file>      Specify configuration file which should be used for reading
                  Confluence page URL and markdown file path.
  -k             Lock page editing to current user only to prevent accidental
                  manual edits over Confluence Web UI.
  --dry-run      Show resulting HTML and don't update Confluence page .
  --trace        Enable trace logs.
  -h --help      Show this screen and call 911.
  -v --version   Show version.
`
)

type StorageInfo struct {
	Value string `json:"value"`
	Representation string `json:"representation"`
}

type PageInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	
	Body struct {
		Storage StorageInfo `json:"storage",omitempty`
		View StorageInfo `json:"view",omitempty`
	} `json:"body"`

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
type AttachmentInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	
	Data  []byte `json:"__data"`

	Status string `json:"status"`
	Links struct {
		Full string `json:"webui"`
		Download string `json:"download"`
	} `json:"_links"`
}

const (
	HeaderParent string = `Parent`
	HeaderSpace         = `Space`
	HeaderTitle         = `Title`
	HeaderLayout        = `Layout`
)

type Meta struct {
	Parents []string
	Space   string
	Title   string
	Layout  string
}

var (
	logger = lorg.NewLog()
)


func formatXML(data []byte) ([]byte, error) {
    b := &bytes.Buffer{}
    decoder := xml.NewDecoder(bytes.NewReader(data))
    encoder := xml.NewEncoder(b)
    encoder.Indent("", "  ")
    for {
		token, err := decoder.Token()
		
		
		startElement, ok := token.(xml.StartElement)
		if ok &&  startElement.Name.Space== "ac" && strings.HasPrefix(startElement.Name.Local , "layout") {
			continue
		}

		if ok &&  startElement.Name.Space== "ac" &&  startElement.Name.Local == "structured-macro" {
			// remove macro-id
			a := startElement.Attr;
			for i := 0; i < len(a); i ++ {
				attribute := a[i]
				if attribute.Name.Space == "ac" && attribute.Name.Local == "macro-id" {
					a[i] = a[len(a)-1]
					a = a[:len(a)-1]
					startElement.Attr = a
					token = startElement
					break;
				}
			}
		}

		endElement, ok := token.(xml.EndElement)
		if ok &&  endElement.Name.Space== "ac" && strings.HasPrefix(endElement.Name.Local , "layout") {
			continue
		}



	 
        if err == io.EOF {
            encoder.Flush()
            return b.Bytes(), nil
        }
        if err != nil {
            return nil, err
        }
        err = encoder.EncodeToken(token)
        if err != nil {
            return nil, err
        }
    }
}

func main() {
	args, err := godocs.Parse(usage, "mark 1.0", godocs.UsePager)
	if err != nil {
		panic(err)
	}

	var (
		username, _   = args["-u"].(string)
		password, _   = args["-p"].(string)
		targetURL, _  = args["-l"].(string)
		targetFile, _ = args["-f"].(string)
		dryRun        = args["--dry-run"].(bool)
		editLock      = args["-k"].(bool)
		trace         = args["--trace"].(bool)

		optionsFile, shouldReadOptions = args["-c"].(string)
	)

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

	config, err := getConfig(filepath.Join(os.Getenv("HOME"), ".config/mark"))
	if err != nil && !os.IsNotExist(err) {
		logger.Fatal(err)
	}

	if shouldReadOptions {
		optionsConfig, err := getConfig(optionsFile)
		if err != nil {
			logger.Fatalf(
				"can't read options config '%s': %s", optionsFile, err,
			)
		}

		targetURL, err = optionsConfig.GetString("url")
		if err != nil {
			logger.Fatal(
				"can't read `url` value from options file (%s): %s",
				optionsFile, err,
			)
		}

		targetFile, err = optionsConfig.GetString("file")
		if err != nil {
			logger.Fatal(
				"can't read `file` value from options file (%s): %s",
				optionsFile, err,
			)
		}
	}

	markdownData, err := ioutil.ReadFile(targetFile)
	if err != nil {
		logger.Fatal(err)
	}

	meta, err := extractMeta(markdownData)
	if err != nil {
		logger.Fatal(err)
	}

	images := map[string]MacroImage {}
	htmlData := compileMarkdown(markdownData, filepath.Dir(targetFile), &images)

	
	if dryRun {
		fmt.Println(string(htmlData))
		os.Exit(0)
	}

	if username == "" {
		username, err = config.GetString("username")
		if err != nil {
			if zhash.IsNotFound(err) {
				logger.Fatal(
					"Confluence username should be specified using -u " +
						"flag or be stored in configuration file",
				)
			}

			logger.Fatalf(
				"can't read username configuration variable: %s", err,
			)
		}
	}

	if password == "" {
		password, err = config.GetString("password")
		if err != nil {
			if zhash.IsNotFound(err) {
				logger.Fatal(
					"Confluence password should be specified using -p " +
						"flag or be stored in configuration file",
				)
			}

			logger.Fatalf(
				"can't read password configuration variable: %s", err,
			)
		}
	}

	url, err := url.Parse(targetURL)
	if err != nil {
		logger.Fatal(err)
	}

	baseURL := url.Scheme + "://" + url.Host

	if url.Host == "" {
		baseURL, err = config.GetString("base_url")
		if err != nil {
			if zhash.IsNotFound(err) {
				logger.Fatal(
					"Confluence base URL should be specified using -l " +
						"flag or be stored in configuration file",
				)
			}

			logger.Fatalf(
				"can't read base_url configuration variable: %s", err,
			)
		}
	}

	baseURL = strings.TrimRight(baseURL, `/`)

	api := NewAPI(baseURL, username, password)

	pageID := url.Query().Get("pageId")

	if pageID != "" && meta != nil {
		logger.Warningf(
			`specified file contains metadata, ` +
				`but it will be ignore due specified command line URL`,
		)

		meta = nil
	}

	if pageID == "" && meta == nil {
		logger.Fatalf(
			`specified file doesn't contain metadata ` +
				`and URL is not specified via command line ` +
				`or doesn't contain pageId GET-parameter`,
		)
	}

	var target *PageInfo

	if meta != nil {
		page, err := resolvePage(api, meta)
		if err != nil {
			logger.Fatal(err)
		}

		target = page
	} else {
		if pageID == "" {
			logger.Fatalf("URL should provide 'pageId' GET-parameter")
		}

		page, err := api.getPageByID(pageID)
		if err != nil {
			logger.Fatal(err)
		}

		target = page
	}

	 //  any changes?
	fromattedExisting, err := formatXML([]byte(target.Body.Storage.Value))
	fromattedNew, err := formatXML(htmlData)
	if string(fromattedExisting) != string(fromattedNew) {
		logger.Debug("Updating page")
		err = api.updatePage(
			target,
			MacroLayout{meta.Layout, [][]byte{htmlData}}.Render(),
		)
		if err != nil {
			logger.Fatal(err)
		}
	}  

	// Uplodad images
	for _, imageMacro := range images {
		
		existingAttachment, _ := api.getAttachment(target.ID, imageMacro.Path)
		if existingAttachment == nil  {
			api.ensureAttachment(target.ID, imageMacro.Path, imageMacro.Title, imageMacro.Data, nil)
		} else {
			api.ensureAttachment(target.ID, imageMacro.Path, imageMacro.Title, imageMacro.Data, existingAttachment)
		}
    }

	if editLock {
		logger.Infof(
			`edit locked on page '%s' by user '%s' to prevent manual edits`,
			target.Title,
			username,
		)

		err := api.setPagePermissions(target, RestrictionEdit, []Restriction{
			{User: username},
		})
		if err != nil {
			logger.Fatal(err)
		}
	}

	fmt.Printf(
		"page successfully updated: %s\n",
		baseURL+target.Links.Full,
	)

}
 
// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because blackfriday markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> for whatever reason.
func compileMarkdown(markdown []byte, basePath string,  images *map[string]MacroImage) []byte {
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
		basePath,
		images, 
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

func resolvePage(api *API, meta *Meta) (*PageInfo, error) {
	page, err := api.findPage(meta.Space, meta.Title)
	if err != nil {
		return nil, ser.Errorf(
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
		page, err := validateAncestry(
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

	parent, err := ensureAncestry(
		api,
		meta.Space,
		meta.Parents,
	)
	if err != nil {
		return nil, ser.Errorf(
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
		page, err := api.createPage(meta.Space, parent, meta.Title, ``)
		if err != nil {
			return nil, ser.Errorf(
				err,
				"can't create page '%s': %s",
				meta.Title,
			)
		}

		return page, nil
	}

	return page, nil
}

func ensureAncestry(
	api *API,
	space string,
	ancestry []string,
) (*PageInfo, error) {
	var parent *PageInfo

	rest := ancestry

	for i, title := range ancestry {
		page, err := api.findPage(space, title)
		if err != nil {
			return nil, ser.Errorf(
				err,
				`error during finding parent page with title '%s': %s`,
				title,
			)
		}

		if page == nil {
			break
		}

		logger.Tracef("parent page '%s' exists: %s", title, page.Links.Full)

		rest = ancestry[i:]
		parent = page
	}

	if parent != nil {
		rest = rest[1:]
	} else {
		page, err := api.findRootPage(space)
		if err != nil {
			return nil, ser.Errorf(
				err,
				"can't find root page for space '%s': %s", space,
			)
		}

		parent = page
	}

	if len(rest) == 0 {
		return parent, nil
	}

	logger.Debugf(
		"empty pages under '%s' to be created: %s",
		parent.Title,
		strings.Join(rest, ` > `),
	)

	for _, title := range rest {
		page, err := api.createPage(space, parent, title, ``)
		if err != nil {
			return nil, ser.Errorf(
				err,
				`error during creating parent page with title '%s': %s`,
				title,
			)
		}

		parent = page
	}

	return parent, nil
}

func validateAncestry(
	api *API,
	space string,
	ancestry []string,
) (*PageInfo, error) {
	page, err := api.findPage(space, ancestry[len(ancestry)-1])
	if err != nil {
		return nil, err
	}

	if page == nil {
		return nil, nil
	}

	if len(page.Ancestors) < 1 {
		return nil, fmt.Errorf(`page '%s' has no parents`, page.Title)
	}

	if len(page.Ancestors) < len(ancestry) {
		return nil, fmt.Errorf(
			"page '%s' has fewer parents than specified: %s",
			page.Title,
			strings.Join(ancestry, ` > `),
		)
	}

	// skipping root article title
	for i, ancestor := range page.Ancestors[1:len(ancestry)] {
		if ancestor.Title != ancestry[i] {
			return nil, fmt.Errorf(
				"broken ancestry tree; expected tree: %s; "+
					"encountered '%s' at position of '%s'",
				strings.Join(ancestry, ` > `),
				ancestor.Title,
				ancestry[i],
			)
		}
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

		return zhash.NewHash(), ser.Errorf(
			err,
			"can't decode toml file: %s",
		)
	}

	return zhash.HashFromMap(configData), nil
}

func extractMeta(data []byte) (*Meta, error) {
	headerPattern := regexp.MustCompile(`\[\]:\s*#\s*\(([^:]+):\s*(.*)\)`)

	var meta *Meta

	scanner := bufio.NewScanner(bytes.NewBuffer(data))
	for scanner.Scan() {
		line := scanner.Text()

		if err := scanner.Err(); err != nil {
			return nil, err
		}

		matches := headerPattern.FindStringSubmatch(line)
		if matches == nil {
			break
		}

		if meta == nil {
			meta = &Meta{}
		}

		header := strings.Title(matches[1])

		switch header {
		case HeaderParent:
			meta.Parents = append(meta.Parents, matches[2])

		case HeaderSpace:
			meta.Space = strings.ToUpper(matches[2])

		case HeaderTitle:
			meta.Title = strings.TrimSpace(matches[2])

		case HeaderLayout:
			meta.Layout = strings.TrimSpace(matches[2])

		default:
			logger.Errorf(
				`encountered unknown header '%s' line: %#v`,
				header,
				line,
			)

			continue
		}
	}

	if meta == nil {
		return nil, nil
	}

	if meta.Space == "" {
		return nil, fmt.Errorf(
			"space key is not set (%s header is not set)",
			HeaderSpace,
		)
	}

	if meta.Title == "" {
		return nil, fmt.Errorf(
			"page title is not set (%s header is not set)",
			HeaderTitle,
		)
	}

	return meta, nil
}
