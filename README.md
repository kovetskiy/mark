# Mark

<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-44-orange.svg?style=flat-square)](#contributors-)
<!-- ALL-CONTRIBUTORS-BADGE:END -->

Mark — a tool for syncing your markdown documentation with Atlassian Confluence
pages.

Read the blog post discussing the tool — https://samizdat.dev/use-markdown-for-confluence/

This is very useful if you store documentation to your software in a Git
repository and don't want to do an extra job of updating Confluence page using
a tinymce wysiwyg enterprise core editor which always breaks everything.

Mark does the same but in a different way. Mark reads your markdown file, creates a Confluence page
if it's not found by its name, uploads attachments, translates Markdown into HTML and updates the
contents of the page via REST API. It's like you don't even need to create sections/pages in your
Confluence anymore, just use them in your Markdown documentation.

Mark uses an extended file format, which, still being valid markdown,
contains several HTML-ish metadata headers, which can be used to locate page inside
Confluence instance and update it accordingly.

File in the extended format should follow the specification:
```markdown
<!-- Space: <space key> -->
<!-- Parent: <parent 1> -->
<!-- Parent: <parent 2> -->
<!-- Title: <title> -->
<!-- Attachment: <local path> -->
<!-- Label: <label 1> -->
<!-- Label: <label 2> -->

<page contents>
```

There can be any number of `Parent` headers, if Mark can't find specified
parent by title, Mark creates it.

Also, optional following headers are supported:

```markdown
<!-- Layout: (article|plain) -->
```

* (default) article: content will be put in narrow column for ease of
  reading;
* plain: content will fill all page;

```markdown
<!-- Type: (page|blogpost) -->
```

* (default) page: normal Confluence page - defaults to this if omitted
* blogpost: [Blog post](https://confluence.atlassian.com/doc/blog-posts-834222533.html) in `Space`.  Cannot have `Parent`(s)

```markdown
<!-- Content-Appearance: (full-width|fixed) -->
```

* (default) full-width: content will fill the full page width
* fixed: content will be rendered in a fixed narrow view

```markdown
<!-- Sidebar: <h2>Test</h2> -->
```

Setting the sidebar creates a column on the right side.  You're able to add any valid HTML content. Adding this property sets the layout to `article`.

Mark supports Go templates, which can be included into article by using path
to the template relative to current working dir, e.g.:

```markdown
<!-- Include: <path> -->
```

If the template cannot be found relative to the current directory, a fallback directory can be defined via `--include-path`. This way it is possible to have global include files while local ones will still take precedence.

Optionally the delimiters can be defined:

```markdown
<!-- Include: <path>
     Delims: "<<", ">>"
     -->
```

Or they can be switched off to disable processing:

```markdown
<!-- Include: <path>
     Delims: none
     -->
```

**Note:** Switching delimiters off really simply changes
them to ASCII characters "\x00" and "\x01" which, usually
should not occure in a template.

Templates can accept configuration data in YAML format which immediately
follows the `Include` and `Delims` tag, if present:

```markdown
<!-- Include: <path>
     <yaml-data> -->
```

Mark also supports attachments. The standard way involves declaring an
`Attachment` along with the other items in the header, then have any links
with the same path:

```markdown
<!-- Attachment: <path-to-image> -->

<beginning of page content>

An attached link is [here](<path-to-image>)
```

**NOTE**: Be careful with `Attachment`! If your path string is a subset of
another longer string or referenced in text, you may get undesired behavior.

Mark also supports macro definitions, which are defined as regexps which will
be replaced with specified template:

```markdown
<!-- Macro: <regexp>
     Template: <path>
     <yaml-data> -->
```

**NOTE**: Make sure to define your macros after your metadata (Title/Space),
mark will stop processing metadata if it hits a Macro.

Capture groups can be defined in the macro's <regexp> which can be later
referenced in the `<yaml-data>` using `${<number>}` syntax, where `<number>` is
number of a capture group in regexp (`${0}` is used for entire regexp match),
for example:

```markdown
  <!-- Macro: MYJIRA-\d+
       Template: ac:jira:ticket
       Ticket: ${0} -->
```

Macros can also use inline templates.
Inline templates are templates where the template content
is described in the `<yaml-data>`.
The `Template` value starts with a `#`, followed by the key
used in the `<yaml-data>`.
The key's value must be a string which defines the template's content.

```markdown
  <!-- Macro: <tblbox\s+(.*?)\s*>
       Template: #inline
       title: ${1}
       inline: |
           <table>
           <thead><tr><th>{{ .title }}</th></tr></thead>
           <tbody><tr><td>
        -->
  <!-- Macro: </tblbox>
       Template: #also_inline
       also_inline: |
           </td></tr></tbody></table>
        -->
  <tblbox with a title>
  and some
  content
  </tblbox>
```

### Customizing the page layout

If you set the Layout to plain, the page layout can be customized using HTML comments inside the markdown:

```markdown
<!-- Layout: plain -->
<!-- ac:layout -->

<!-- ac:layout-section type:three_with_sidebars -->
<!-- ac:layout-cell -->
More Content
<!-- ac:layout-cell end -->
<!-- ac:layout-cell -->
More Content
<!-- ac:layout-cell end -->
<!-- ac:layout-cell -->
Even More Content
<!-- ac:layout-cell end -->
<!-- ac:layout-section end -->

<!-- ac:layout-section type:single -->
<!-- ac:layout-cell -->
Still More Content
<!-- ac:layout-cell end -->
<!-- ac:layout-section end -->

<!-- ac:layout end -->
```

Please be aware that mark does not validate the layout, so it's your responsibility to create a valid layout.

### Code Blocks

If you have long code blocks, you can make them collapsible with the [Code Block Macro]:

    ```bash collapse
    ...
    some long bash code block
    ...
    ```

And you can also add a title:

    ```bash collapse title Some long long bash function
    ...
    some long bash code block
    ...
    ```

Or linenumbers, by giving the first number

    ```bash 1 collapse title Some long long bash function
    ...
    some long bash code block
    ...
    ```

And even themes

    ```bash 1 collapse midnight title Some long long bash function
    ...
    some long bash code block
    ...
    ```

Please note that, if you want to have a code block without a language
use `-` as the first character, if you want to have the other goodies

    ``` - 1 collapse midnight title Some long long code
    ...
    some long code block
    ...
    ```

[Code Block Macro]: https://confluence.atlassian.com/doc/code-block-macro-139390.html

### Block Quotes

Block Quotes are converted to Confluence Info/Warn/Note box when the following conditions are met

1. The BlockQuote is on the root level of the document (not nested)
1. The first line of the BlockQuote contains one of the following patterns `Info/Warn/Note`

In any other case the default behaviour will be resumed and html `<blockquote>` tag will be used

## Template & Macros

By default, mark provides several built-in templates and macros:

* template `ac:status` to include badge-like text, which accepts following
  parameters:
  - Title: text to display in the badge
  - Color: color to use as background/border for badge
    - Grey
    - Red
    - Yellow
    - Green
    - Blue
  - Subtle: specify to fill badge with background or not
    - true
    - false

* template `ac:box`to include info, tip, note, and warning text boxes. Parameters:
  - Name: select box style
    - info
    - tip
    - note
    - warning
  - Icon: show information/tip/exclamation mark/warning icon
    - true
    - false
  - Title: title text of the box
  - Body: text to display in the box

  See: https://confluence.atlassian.com/conf59/info-tip-note-and-warning-macros-792499127.html

* template `ac:jira:ticket` to include JIRA ticket link. Parameters:
  - Ticket: Jira ticket number like BUGS-123.

  See: https://confluence.atlassian.com/conf59/status-macro-792499207.html

* template `ac:jira:filter` to include JIRA Filters/Searches. Parameters:
  - JQL: The "JQL" query of the search
  - Server (Optional): The Jira server to fetch the query from if its not the default of "System Jira"

* template `ac:jiraissues` to include a list of JIRA tickets. Parameters:
  - URL (Required), The URL of the XML view of your selected issues. (link to the filter)
  - Anonymous (Optional) If this parameter is set to 'true', your JIRA application will return only the issues which allow unrestricted viewing. That is, the issues which are visible to anonymous viewers. If this parameter is omitted or set to 'false', then the results depend on how your administrator has configured the communication between the JIRA application and Confluence. By default, Confluence will show only the issues which the user is authorised to view.
  - BaseURL  (Optional) If you specify a 'baseurl', then the link in the header, pointing to your JIRA application, will use this base URL instead of the value of the 'url' parameter. This is useful when Confluence connects to JIRA with a different URL from the one used by other users.
  - Columns  (Optional) A list of JIRA column names, separated by semi-colons (;). You can include many columns recognized by your JIRA application, including custom columns.
  - Count  (Optional) If this parameter is set to 'true', the issue list will show the number of issues in JIRA. The count will be linked to your JIRA site.
  - Cache  (Optional) The macro maintains a cache of the issues which result from the JIRA query. If the 'cache' parameter is set to 'off', the relevant part of the cache is cleared each time the macro is reloaded. (The value 'false' also works and has the same effect as 'off'.)
  - Height  (Optional) The height in pixels of the table displaying the issues.
  - RenderMode  (Optional) If the value is 'dynamic', the JIRA Issues macro offers an interactive display.
  - Title  (Optional) You can customise the title text at the top of the issues table with this parameter. For instance, setting the title to 'Bugs-to-fix' will replace the default 'JIRA Issues' text. This can help provide more context to the list of issues displayed.
  - Width  (Optional) The width of the table displaying the issues. Can be entered as a percentage (%) or in pixels (px).

  See: https://confluence.atlassian.com/doc/jira-issues-macro-139380.html

* template: `ac:emoticon` to include emoticons. Parameters:
  - Name: select emoticon
    - smile
    - sad
    - cheeky
    - laugh
    - wink
    - thumbs-up
    - thumbs-down
    - information
    - tick
    - cross
    - warning
    - plus
    - minus
    - question
    - light-on
    - light-off
    - yellow-star
    - red-star
    - green-star
    - blue-star

  See: https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html

* template: `ac:youtube` to include YouTube Widget. Parameters:
  - URL: YouTube video endpoint
  - Width: Width in px. Defaults to "640px"
  - Height: Height in px. Defaults to "360px"

  See: https://confluence.atlassian.com/doc/widget-connector-macro-171180449.html#WidgetConnectorMacro-YouTube

* template: `ac:children` to include Children Display macro
	- Reverse (Reverse Sort): Use with the `Sort Children By` parameter. When set, the sort order changes from ascending to descending.
      - `true`
      - `false` (Default)
	- Sort (Sort Children By):
      - `creation` — to sort by content creation date
      - `title` — to sort alphabetically on title
      - `modified` — to sort of last modification date.
      - If not specified, manual sorting is used if manually ordered, otherwise alphabetical.
	- Style (Heading Style): Choose the style used to display descendants.
      - from `h1` to `h6`
      - If not specified, default style is applied.
	- Page (Parent Page):
      - `/` — to list the top-level pages of the current space, i.e. those without parents.
      - `pagename` — to list the children of the specified page.
      - `spacekey:pagename` — to list the children of the specified page in the specified space.
      - If not specified, the current page is used.
	- Excerpt (Include Excerpts): Allows you to include a short excerpt under each page in the list.
      - `none` - no excerpt will be displayed. (Default)
      - `simple` - displays the first line of text contained in an Excerpt macro any of the returned pages. If there is not an Excerpt macro on the page, nothing will be shown.
      - `rich content` - displays the contents of an Excerpt macro, or if there is not an Excerpt macro on the page, the first part of the page content, including formatted text, images and some macros.
	- First (Number of Children): Restrict the number of child pages that are displayed at the top level.
      - If not specified, no limit is applied.
	- Depth (Depth of Descendants): Enter a number to specify the depth of descendants to display. For example, if the value is 2, the macro will display 2 levels of child pages. This setting has no effect if `Show Descendants` is enabled.
      - If not specified, no limit is applied.
	- All (Show Descendants): Choose whether to display all the parent page's descendants.
      - `true`
      - `false` (Default)

  See: https://confluence.atlassian.com/doc/children-display-macro-139501.html

* template: `ac:iframe` to include iframe macro (cloud only)
  - URL: URL to the iframe.
  - Frameborder: Choose whether to draw a border around content in the iframe.
      - `show` (Default)
      - `hide`
  - Width: Width in px. Defaults to "640px"
  - Height: Height in px. Defaults to "360px"
  - Scrolling: Allow or prevent scrolling in the iframe to see additional content.
      - `yes`
      - `no`
      - `auto` (Default)
  - Align: Align the iframe to the left or right of the page.
      - `left` (Default)
      - `right`

  See: https://support.atlassian.com/confluence-cloud/docs/insert-the-iframe-macro

* template: `ac:blog-posts`to include blog-posts
  - Content: How much content will be shown
      - titles (default)
      - excerpts
      - entire
  - Time: Specify how much back in time Confluence should look for blog posts (default: unlimited)
  - Label: Restrict to blog posts with specific labels
  - Author: Restrict to blog posts by specific authors
  - Spaces: Restrict to blog posts in specific spaces
  - Max: Maximum number of blog posts shown (default: 15)
  - Sort: Sorting posts by
      - title
      - creation (default)
      - modified
  - Reverse: Reverses the Sort parameter from oldest to newest (default: false)

  See: https://confluence.atlassian.com/doc/blog-posts-macro-139470.html

* template: `ac:include` to include a page
  - Page: the page to be included
  - Space: the space the page is in (optional, otherwise same space)

* template: `ac:excerpt-include` to include the excerpt from another page
  - Page: the page the excerpt should be included from
  - NoPanel: Determines whether Confluence will display a panel around the excerpted content (optional, default: false)

* template: `ac:excerpt` to create an excerpt and include it in the page
  - Excerpt: The text you want to include
  - OutputType: Determines whether the content of the Excerpt macro body is displayed on a new line or inline (optional, options: "BLOCK" or "INLINE", default: BLOCK)
  - Hidden: Hide the excerpt content (optional, default: false)

* template: `ac:anchor` to set an anchor inside a page
  - Anchor: Text for the anchor

+ template: `ac:expand` to display an expandable/collapsible section of text on your page
  - Title: Defines the text next to the expand/collapse icon.
  - Body: The Text that it is expanded to.

* template: `ac:profile` to display a short summary of a given Confluence user's profile.
  - Name: The username of the Confluence user whose profile summary you wish to show.

* template: `ac:contentbylabel` to display a list of pages, blog posts or attachments that have particular labels
  - CQL: The CQL query to discover the content

* template: `ac:detailssummary` to show summary information from one page on a another page
  - Headings: Column headings to show
  - CQL: The CQL query to discover the pages
  - SortBy: Sort by a specific column heading

* template: `ac:details` to create page properties
  - Body: Must contain a table with two rows, the table headings are used as property key. The table content is the value.

* template: `ac:panel` to display a block of text within a customisable panel
  - Title: Panel title (optional)
  - Body:  Body text of the panel
  - BGColor: Background Color
  - TitleBGColor: Background color of the title bar
  - TitleColor: Text color of the title
  - BorderStyle: Style of the panel's border

* template `ac:recently-updated` to display a list of most recently changed content
  - Spaces: List of Spaces to watch (optional, default is current Space)
  - ShowProfilePic: Show profile picture of editor
  - Max: Maximum number of changes
  - Types: Include these content types only (comments, blogposts, pages)
  - Theme: Apperance of the macro (concise, social, sidebar)
  - HideHeading: Determines whether the macro hides or displays the text 'Recently Updated' as a title above the list of content
  - Labels: Filter the results by label. The macro will display only the pages etc which are tagged with the label(s) you specify here.

* template: `ac:pagetreesearch` to add a search box to your Confluence page.
  - Root: Name of the root page whose hierarchy of pages will be searched by this macro. If this not specified, the root page is the current page.

* macro `@{...}` to mention user by name specified in the braces.

## Template & Macros Usecases

### Insert Disclaimer

**disclaimer.md**

```markdown
**NOTE**: this document is generated, do not edit manually.
```

**article.md**
```markdown
<!-- Space: TEST -->
<!-- Title: My Article -->

<!-- Include: disclaimer.md -->

This is my article.
```

### Insert Status Badge

**article.md**

```markdown
<!-- Space: TEST -->
<!-- Title: TODO List -->

<!-- Macro: :done:
     Template: ac:status
     Title: DONE
     Color: Green -->

<!-- Macro: :todo:
     Template: ac:status
     Title: TODO
     Color: Blue -->

* :done: Write Article
* :todo: Publish Article
```

### Insert Colored Text Box

**article.md**

```markdown
<!-- Space: TEST -->
<!-- Title: Announcement -->

<!-- Macro: :box:([^:]+):([^:]*):(.+):
     Template: ac:box
     Icon: true
     Name: ${1}
     Title: ${2}
     Body: ${3} -->

:box:info::Foobar:
:box:tip:Tip of day:Foobar:
:box:note::Foobar:
:box:warning:Alert!:Foobar:
```

### Insert Table of Contents

```markdown
<!-- Include: ac:toc -->
```

If default TOC looks don't find a way to your heart, try [parametrizing it][Confluence TOC Macro], for example:

```markdown
<!-- Macro: :toc:
     Template: ac:toc
     Printable: 'false'
     MinLevel: 2 -->

# This is my nice title

:toc:
```

You can call the `Macro` as you like but the `Template` field must have the `ac:toc` value.
Also, note the single quotes around `'false'`.

See [Confluence TOC Macro] for the list of parameters - keep in mind that here
they start with capital letters. Every skipped field will have the default
value, so feel free to include only the ones that you require.

[Confluence TOC Macro]:https://confluence.atlassian.com/conf59/table-of-contents-macro-792499210.html

### Insert PageTree

```markdown
# My First Heading
<!-- Include: ac:pagetree -->
```

The pagetree macro works almost the same as the TOC above, but the tree behavior
is more desirable for creating placeholder pages above collections of SOPs.

The default pagetree macro behavior is to insert a tree rooted @self.

The following parameters can be used to alter your default configuration with
parameters described more in depth here:[Confluence Pagetree Macro].

Parameters:

* Title (of tree root page)
* Sort
* Excerpt
* Reverse
* SearchBox
* ExpandCollapseAll
* StartDepth

[Confluence Pagetree Macro]:https://confluence.atlassian.com/conf59/page-tree-macro-792499177.html

E.G.
```markdown
<!-- Macro: :pagetree:
     Template: ac:pagetree
     Reverse: 'true'
     ExpandCollapseAll: 'true'
     StartDepth: 2 -->

# My First Heading

:pagetree:
```

### Insert Children Display

To include Children Display (TOC displaying children pages) use following macro:

```markdown
<!-- Macro: :children:
     Template: ac:children
-->

# This is my nicer title

:children:
```

You can use various [parameters](https://confluence.atlassian.com/conf59/children-display-macro-792499081.html) to modify Children Display:

```markdown
<!-- Macro: :children:
     Template: ac:children
     Sort: title
     Style: h3
     Excerpt: simple
     First: 10
     Page: Space:Page title
     Depth: 2
     Reverse: false
     All: false -->

# This is my nicest title

:children:
```
### Insert Jira Ticket

**article.md**

```markdown
<!-- Space: TEST -->
<!-- Title: TODO List -->

<!-- Macro: MYJIRA-\d+
     Template: ac:jira:ticket
     Ticket: ${0} -->

See task MYJIRA-123.
```

### Insert link to existing confluence page by title

```markdown
This is a [link to an existing confluence page](ac:Pagetitle)

And this is how to link when the linktext is the same as the [Pagetitle](ac:)

Link to a [page title containing spaces](<ac:With Multiple Words>)
```

### Upload and included inline images

```markdown
![Example](../images/examples.png)
```
will automatically upload the inlined image as an attachment and inline the image using the `ac:image` template.

If the file is not found, it will inline the image using the `ac:image` template and link to the image.

### Add width for an image

Use the following macro:
```markdown
<!-- Macro: \!\[.*\]\((.+)\)\<\!\-\- width=(.*) \-\-\>
     Template: ac:image
     Attachment: ${1}
     Width: ${2} -->
```
And attach any image with the following
```markdown
![Example](../images/example.png)<!-- width=300 -->
```
The width will be the commented html after the image (in this case 300px).

Currently this is not compatible with the automated upload of inline images.

### Render Mermaid Diagram

Confluence doesn't provide [mermaid.js](https://github.com/mermaid-js/mermaid) support natively. Mark provides a convenient way to enable the feature like [Github does](https://github.blog/2022-02-14-include-diagrams-markdown-files-mermaid/).
As long as you have a code block and are marked as "mermaid", the mark will automatically render it as a PNG image and insert into before the code block.

    ```mermaid title diagrams_example
    graph TD;
    A-->B;
    ```

In order to properly render mermaid, you can choose between the following mermaid providers:

* "mermaid-go" via [mermaid.go](https://github.com/dreampuf/mermaid.go)
* "cloudscript" via [cloudscript-io-mermaid-addon](https://marketplace.atlassian.com/apps/1219878/cloudscript-io-mermaid-addon)

## Installation

### Homebrew

```bash
brew tap kovetskiy/mark
brew install mark
```

### Go Install / Go Get

```bash
go install github.com/kovetskiy/mark@latest
```

For older versions

```bash
go get -v github.com/kovetskiy/mark
```

### Releases

[Download a release from the Releases page](https://github.com/kovetskiy/mark/releases)

### Docker

```bash
$ docker run --rm -i kovetskiy/mark:latest mark <params>
```

### Compile and install using docker-compose

Mostly useful when you intend to enhance `mark`.

```bash
# Create the binary
$ docker-compose run markbuilder
# "install" the binary
$ cp mark /usr/local/bin
```

## Usage

```
USAGE:
   mark [global options] [arguments...]

VERSION:
   9.10.1

DESCRIPTION:
   Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark

GLOBAL OPTIONS:
   --files value, -f value                       use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted). [$MARK_FILES]
   --compile-only                                show resulting HTML and don't update Confluence page content. (default: false) [$MARK_COMPILE_ONLY]
   --dry-run                                     resolve page and ancestry, show resulting HTML and exit. (default: false) [$MARK_DRY_RUN]
   --edit-lock, -k                               lock page editing to current user only to prevent accidental manual edits over Confluence Web UI. (default: false) [$MARK_EDIT_LOCK]
   --drop-h1, --h1_drop                          don't include the first H1 heading in Confluence output. (default: false) [$MARK_H1_DROP]
   --strip-linebreak                             remove linebreaks inside of tags, to accomodate Confluence non-standard behavior (default: false)
   --title-from-h1, --h1_title                   extract page title from a leading H1 heading. If no H1 heading on a page exists, then title must be set in the page metadata. (default: false) [$MARK_H1_TITLE]
   --minor-edit                                  don't send notifications while updating Confluence page. (default: false) [$MARK_MINOR_EDIT]
   --color value                                 display logs in color. Possible values: auto, never. (default: "auto") [$MARK_COLOR]
   --debug                                       enable debug logs. (default: false) [$MARK_DEBUG]
   --trace                                       enable trace logs. (default: false) [$MARK_TRACE]
   --username value, -u value                    use specified username for updating Confluence page. [$MARK_USERNAME]
   --password value, -p value                    use specified token for updating Confluence page. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication. [$MARK_PASSWORD]
   --target-url value, -l value                  edit specified Confluence page. If -l is not specified, file should contain metadata (see above). [$MARK_TARGET_URL]
   --base-url value, -b value, --base_url value  base URL for Confluence. Alternative option for base_url config field. [$MARK_BASE_URL]
   --config value, -c value                      use the specified configuration file. (default: System specific) [$MARK_CONFIG]
   --ci                                          run on CI mode. It won't fail if files are not found. (default: false) [$MARK_CI]
   --space value                                 use specified space key. If the space key is not specified, it must be set in the page metadata. [$MARK_SPACE]
   --parents value                               A list containing the parents of the document separated by parents-delimiter (default: '/'). These will be preprended to the ones defined in the document itself. [$MARK_PARENTS]
   --parents-delimiter value                     The delimiter used for the nested parent (default: "/") [$MARK_PARENTS_DELIMITER]
   --mermaid-provider value                      defines the mermaid provider to use. Supported options are: cloudscript, mermaid-go. (default: "cloudscript") [$MARK_MERMAID_PROVIDER]
   --mermaid-scale value                         defines the scaling factor for mermaid renderings. (default: 1) [$MARK_MERMAID_SCALE]
   --include-path value                          Path for shared includes, used as a fallback if the include doesn't exist in the current directory. [$MARK_INCLUDE_PATH]
   --help, -h                                    show help
   --version, -v                                 print the version
```

You can store user credentials in the configuration file, which should be
located in a system specific directory (or specified via `-c --config <path>`) with the following format (TOML):

```toml
username = "your-email"
password = "password-or-api-key-for-confluence-cloud"
# If you are using Confluence Cloud add the /wiki suffix to base_url
base-url = "http://confluence.local"
title-from-h1 = true
drop-h1 = true
```

**NOTE**: Labels aren't supported when using `minor-edit`!

**NOTE**: The system specific locations are described in here:
https://pkg.go.dev/os#UserConfigDir.
Currently these are:
On Unix systems, it returns $XDG_CONFIG_HOME as specified by https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html if non-empty, else $HOME/.config. On Darwin, it returns $HOME/Library/Application Support. On Windows, it returns %AppData%. On Plan 9, it returns $home/lib.

# Tricks

## Continuous Integration

It's quite trivial to integrate Mark into a CI/CD system, here is an example with [Snake CI](https://snake-ci.com/)
in case of self-hosted Bitbucket Server / Data Center.

```yaml
stages:
  - sync

Sync documentation:
  stage: sync
  only:
    branches:
      - main
  image: kovetskiy/mark
  commands:
    - for file in $(find -type f -name '*.md'); do
        echo "> Sync $file";
        mark -u $MARK_USER -p $MARK_PASS -b $MARK_URL -f $file || exit 1;
        echo;
      done
```

In this example, I'm using the `kovetskiy/mark` image for creating a job container where the
repository with documentation will be cloned to. The following command finds all `*.md` files and runs mark against them one by one:

```bash
for file in $(find -type f -name '*.md'); do
    echo "> Sync $file";
    mark -u $MARK_USER -p $MARK_PASS -b $MARK_URL -f $file || exit 1;
    echo;
done
```

The following directive tells the CI to run this particular job only if the changes are pushed into the
`main` branch. It means you can safely push your changes into feature branches without being afraid
that they automatically shown in Confluence, then go through the reviewal process and automatically
deploy them when PR got merged.

```yaml
only:
  branches:
    - main
```

## File Globbing

Rather than running `mark` multiple times, or looping through a list of files from `find`, you can use file globbing (i.e. wildcard patterns) to match files in subdirectories. For example:
```bash
mark -f "helpful_cmds/*.md"
```

## Issues, Bugs & Contributions

I've started the project to solve my own problem and open sourced the solution so anyone who has a problem like me can solve it too.
I have no profits/sponsors from this projects which means I don't really prioritize working on this project in my free time.
I still check the issues and do code reviews for Pull Requests which means if you encounter a bug in
the program, you should not expect me to fix it as soon as possible, but I'll be very glad to
merge your own contributions into the project and release the new version.

I try to label all new issues so it's easy to find a bug or a feature request to fix/implement, if
you are willing to help with the project, you can use the following labels to find issues, just make
sure to reply in the issue to let everyone know you took the issue:

- [label:feature-request](https://github.com/kovetskiy/mark/issues?q=is%3Aissue+is%3Aopen+label%3Afeature-request)
- [label:bug](https://github.com/kovetskiy/mark/issues?q=is%3Aissue+is%3Aopen+label%3Abug)

## Contributors ✨

Thanks goes to these wonderful people ([emoji key](https://allcontributors.org/docs/en/emoji-key)):

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://mastodon.social/@mrueg"><img src="https://avatars.githubusercontent.com/u/489370?v=4?s=100" width="100px;" alt="Manuel Rüger"/><br /><sub><b>Manuel Rüger</b></sub></a><br /><a href="#maintenance-mrueg" title="Maintenance">🚧</a> <a href="https://github.com/kovetskiy/mark/commits?author=mrueg" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/kovetskiy"><img src="https://avatars.githubusercontent.com/u/8445924?v=4?s=100" width="100px;" alt="Egor Kovetskiy"/><br /><sub><b>Egor Kovetskiy</b></sub></a><br /><a href="#maintenance-kovetskiy" title="Maintenance">🚧</a> <a href="https://github.com/kovetskiy/mark/commits?author=kovetskiy" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://klauer.dev/"><img src="https://avatars.githubusercontent.com/u/4735?v=4?s=100" width="100px;" alt="Nick Klauer"/><br /><sub><b>Nick Klauer</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=klauern" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/rofafor"><img src="https://avatars.githubusercontent.com/u/9297850?v=4?s=100" width="100px;" alt="Rolf Ahrenberg"/><br /><sub><b>Rolf Ahrenberg</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=rofafor" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/csoutherland"><img src="https://avatars.githubusercontent.com/u/840471?v=4?s=100" width="100px;" alt="Charles Southerland"/><br /><sub><b>Charles Southerland</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=csoutherland" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/snejus"><img src="https://avatars.githubusercontent.com/u/16212750?v=4?s=100" width="100px;" alt="Šarūnas Nejus"/><br /><sub><b>Šarūnas Nejus</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=snejus" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/brnv"><img src="https://avatars.githubusercontent.com/u/1925213?v=4?s=100" width="100px;" alt="Alexey Baranov"/><br /><sub><b>Alexey Baranov</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=brnv" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/princespaghetti"><img src="https://avatars.githubusercontent.com/u/2935312?v=4?s=100" width="100px;" alt="Anthony Barbieri"/><br /><sub><b>Anthony Barbieri</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=princespaghetti" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/dauc"><img src="https://avatars.githubusercontent.com/u/29129213?v=4?s=100" width="100px;" alt="Devin Auclair"/><br /><sub><b>Devin Auclair</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=dauc" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://gezimsejdiu.github.io/"><img src="https://avatars.githubusercontent.com/u/5259296?v=4?s=100" width="100px;" alt="Gezim Sejdiu"/><br /><sub><b>Gezim Sejdiu</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=GezimSejdiu" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/jcavar"><img src="https://avatars.githubusercontent.com/u/3751289?v=4?s=100" width="100px;" alt="Josip Ćavar"/><br /><sub><b>Josip Ćavar</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jcavar" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Hi-Fi"><img src="https://avatars.githubusercontent.com/u/1499780?v=4?s=100" width="100px;" alt="Juho Saarinen"/><br /><sub><b>Juho Saarinen</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Hi-Fi" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/lukiffer"><img src="https://avatars.githubusercontent.com/u/2278911?v=4?s=100" width="100px;" alt="Luke Fritz"/><br /><sub><b>Luke Fritz</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=lukiffer" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/MattyRad"><img src="https://avatars.githubusercontent.com/u/1143595?v=4?s=100" width="100px;" alt="Matt Radford"/><br /><sub><b>Matt Radford</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=MattyRad" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Planktonette"><img src="https://avatars.githubusercontent.com/u/5514719?v=4?s=100" width="100px;" alt="Planktonette"/><br /><sub><b>Planktonette</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Planktonette" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="http://www.stefanoteodorani.it/"><img src="https://avatars.githubusercontent.com/u/2573389?v=4?s=100" width="100px;" alt="Stefano Teodorani"/><br /><sub><b>Stefano Teodorani</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=teopost" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/tillepille"><img src="https://avatars.githubusercontent.com/u/16536696?v=4?s=100" width="100px;" alt="Tim Schrumpf"/><br /><sub><b>Tim Schrumpf</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=tillepille" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/tyler-copilot"><img src="https://avatars.githubusercontent.com/u/18539108?v=4?s=100" width="100px;" alt="Tyler Cole"/><br /><sub><b>Tyler Cole</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=tyler-copilot" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/elgreco247"><img src="https://avatars.githubusercontent.com/u/8968417?v=4?s=100" width="100px;" alt="elgreco247"/><br /><sub><b>elgreco247</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=elgreco247" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/emead-indeed"><img src="https://avatars.githubusercontent.com/u/44018145?v=4?s=100" width="100px;" alt="emead-indeed"/><br /><sub><b>emead-indeed</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=emead-indeed" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://wbhegedus.me/"><img src="https://avatars.githubusercontent.com/u/11506822?v=4?s=100" width="100px;" alt="Will Hegedus"/><br /><sub><b>Will Hegedus</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=wbh1" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/carnei-ro"><img src="https://avatars.githubusercontent.com/u/42899277?v=4?s=100" width="100px;" alt="Leandro Carneiro"/><br /><sub><b>Leandro Carneiro</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=carnei-ro" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/beeme1mr"><img src="https://avatars.githubusercontent.com/u/682996?v=4?s=100" width="100px;" alt="beeme1mr"/><br /><sub><b>beeme1mr</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=beeme1mr" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Taldrain"><img src="https://avatars.githubusercontent.com/u/1081600?v=4?s=100" width="100px;" alt="Taldrain"/><br /><sub><b>Taldrain</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Taldrain" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="http://www.devin.com.br/"><img src="https://avatars.githubusercontent.com/u/349457?v=4?s=100" width="100px;" alt="Hugo Cisneiros"/><br /><sub><b>Hugo Cisneiros</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=eitchugo" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/jevfok"><img src="https://avatars.githubusercontent.com/u/54530686?v=4?s=100" width="100px;" alt="jevfok"/><br /><sub><b>jevfok</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jevfok" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://dev.to/mmiranda"><img src="https://avatars.githubusercontent.com/u/16670310?v=4?s=100" width="100px;" alt="Mateus Miranda"/><br /><sub><b>Mateus Miranda</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=mmiranda" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Skeeve"><img src="https://avatars.githubusercontent.com/u/725404?v=4?s=100" width="100px;" alt="Stephan Hradek"/><br /><sub><b>Stephan Hradek</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Skeeve" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="http://huangx.in/"><img src="https://avatars.githubusercontent.com/u/353644?v=4?s=100" width="100px;" alt="Dreampuf"/><br /><sub><b>Dreampuf</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=dreampuf" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/JAndritsch"><img src="https://avatars.githubusercontent.com/u/190611?v=4?s=100" width="100px;" alt="Joel Andritsch"/><br /><sub><b>Joel Andritsch</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=JAndritsch" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/guoweis-outreach"><img src="https://avatars.githubusercontent.com/u/639243?v=4?s=100" width="100px;" alt="guoweis-outreach"/><br /><sub><b>guoweis-outreach</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=guoweis-outreach" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/klysunkin"><img src="https://avatars.githubusercontent.com/u/2611187?v=4?s=100" width="100px;" alt="klysunkin"/><br /><sub><b>klysunkin</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=klysunkin" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/EppO"><img src="https://avatars.githubusercontent.com/u/6111?v=4?s=100" width="100px;" alt="Florent Monbillard"/><br /><sub><b>Florent Monbillard</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=EppO" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/jfreeland"><img src="https://avatars.githubusercontent.com/u/30938344?v=4?s=100" width="100px;" alt="Joey Freeland"/><br /><sub><b>Joey Freeland</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jfreeland" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/prokod"><img src="https://avatars.githubusercontent.com/u/877414?v=4?s=100" width="100px;" alt="Noam Asor"/><br /><sub><b>Noam Asor</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=prokod" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/PhilippReinke"><img src="https://avatars.githubusercontent.com/u/81698819?v=4?s=100" width="100px;" alt="Philipp"/><br /><sub><b>Philipp</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=PhilippReinke" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/vpommier"><img src="https://avatars.githubusercontent.com/u/8139328?v=4?s=100" width="100px;" alt="Pommier Vincent"/><br /><sub><b>Pommier Vincent</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=vpommier" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/ToruKawaguchi"><img src="https://avatars.githubusercontent.com/u/17423222?v=4?s=100" width="100px;" alt="Toru Kawaguchi"/><br /><sub><b>Toru Kawaguchi</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=ToruKawaguchi" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://coaxialflutter.com/"><img src="https://avatars.githubusercontent.com/u/49793?v=4?s=100" width="100px;" alt="Will Gorman"/><br /><sub><b>Will Gorman</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=willgorman" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://zackery.dev/"><img src="https://avatars.githubusercontent.com/u/15172516?v=4?s=100" width="100px;" alt="Zackery Griesinger"/><br /><sub><b>Zackery Griesinger</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=zgriesinger" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/chrisjaimon2012"><img src="https://avatars.githubusercontent.com/u/57173930?v=4?s=100" width="100px;" alt="cc-chris"/><br /><sub><b>cc-chris</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=chrisjaimon2012" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/datsickkunt"><img src="https://avatars.githubusercontent.com/u/105289244?v=4?s=100" width="100px;" alt="datsickkunt"/><br /><sub><b>datsickkunt</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=datsickkunt" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/recrtl"><img src="https://avatars.githubusercontent.com/u/14078835?v=4?s=100" width="100px;" alt="recrtl"/><br /><sub><b>recrtl</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=recrtl" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/seletskiy"><img src="https://avatars.githubusercontent.com/u/674812?v=4?s=100" width="100px;" alt="Stanislav Seletskiy"/><br /><sub><b>Stanislav Seletskiy</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=seletskiy" title="Code">💻</a></td>
    </tr>
  </tbody>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
