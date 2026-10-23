# Includes, macros and templates

## Includes

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

Includes can be nested inside other included templates. A directive inside an
included file is resolved relative to that file's own directory first, the way
the links it holds are, so `sub/a.md` can include `b.md` to mean `sub/b.md`. If
nothing of that name is there, it is looked for relative to the document and
then in `--include-path`, as it always was, so a nested include written from
the document's directory keeps working. Where both exist, the file beside the
fragment is used and Mark logs a warning naming the one it did not use. Either
way the file has to be inside the document's directory, the directory Mark is
running in, or `--include-path`; a `../` that climbs out of those is refused.
Includes produced by a macro, and the `Template` of a macro defined in an
included file, are resolved relative to the document, since macros are
expanded over the whole document. Furthermore, included files can define page metadata (such as `Title`, `Space`, `Parent`, etc.) or macro definitions. Circular inclusion loops are automatically detected and reported as an error.

## Macros

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

An inline template can use everything a file template can: the `xmlesc` and
`cdata` functions, and the built-in templates by name, such as
`{{ template "ac:status" . }}`. A `${<number>}` written in an inline template
is replaced by the matched text as text, never as template code, so a match
holding `{{ ... }}` is published as written. Inside an action, write it within
a quoted string, as in `{{ "${1}" | xmlesc }}`.

Macro templates can also output `<!-- Include: ... -->` directives, allowing macros to dynamically load external template files or include other documents.

## Whitespace inside `ac:parameter`

A macro parameter holding a storage-format element must hold nothing else, so
Mark publishes such a parameter with the whitespace taken out of it. A template
can be written to be read:

```text
<ac:structured-macro ac:name="inc-drawio">
  <ac:parameter ac:name="name">
    <ri:attachment ri:filename="{{ .Name }}"/>
  </ac:parameter>
</ac:structured-macro>
```

and reaches Confluence as
`<ac:parameter ac:name="name"><ri:attachment ri:filename="..."/></ac:parameter>`.

Left as written, the newlines would make the parameter hold text as well as an
element, and Confluence resolves that by writing the element out as a string:
the page ends up carrying `AttachmentResourceIdentifier[...,filename=...]` where
the attachment should be, with nothing reported, since Mark published what it
was given.

Only a parameter whose value is an element is tightened, which is decided by its
content beginning with `<` and ending with `>`. A parameter holding a string
keeps its spacing, because there the spacing is the value. Whitespace anywhere
else is left alone and has to be: the blank lines inside `<ac:rich-text-body>`
are what let a macro's body be read as Markdown, and a body ending in a list
would otherwise swallow the closing tags.

## Built-in templates and macros

By default, mark provides several built-in templates and macros:

* template `ac:status` to include badge-like text, which accepts following
  parameters:
  * Title: text to display in the badge
  * Color: color to use as background/border for badge
    * Grey
    * Red
    * Yellow
    * Green
    * Blue
  * Subtle: specify to fill badge with background or not
    * true
    * false

* template `ac:box`to include info, tip, note, and warning text boxes. Parameters:
  * Name: select box style
    * info
    * tip
    * note
    * warning
  * Icon: show information/tip/exclamation mark/warning icon
    * true
    * false
  * Title: title text of the box
  * Body: text to display in the box

  See: <https://confluence.atlassian.com/conf59/info-tip-note-and-warning-macros-792499127.html>

* template `ac:jira:ticket` to include JIRA ticket link. Parameters:
  * Ticket: Jira ticket number like BUGS-123.

  See: <https://confluence.atlassian.com/conf59/status-macro-792499207.html>

* template `ac:jira:filter` to include JIRA Filters/Searches. Parameters:
  * JQL: The "JQL" query of the search
  * Server (Optional): The Jira server to fetch the query from if its not the default of "System Jira"

* template `ac:jiraissues` to include a list of JIRA tickets. Parameters:
  * URL (Required), The URL of the XML view of your selected issues. (link to the filter)
  * Anonymous (Optional) If this parameter is set to 'true', your JIRA application will return only the issues which allow unrestricted viewing. That is, the issues which are visible to anonymous viewers. If this parameter is omitted or set to 'false', then the results depend on how your administrator has configured the communication between the JIRA application and Confluence. By default, Confluence will show only the issues which the user is authorised to view.
  * BaseURL  (Optional) If you specify a 'baseurl', then the link in the header, pointing to your JIRA application, will use this base URL instead of the value of the 'url' parameter. This is useful when Confluence connects to JIRA with a different URL from the one used by other users.
  * Columns  (Optional) A list of JIRA column names, separated by semi-colons (;). You can include many columns recognized by your JIRA application, including custom columns.
  * Count  (Optional) If this parameter is set to 'true', the issue list will show the number of issues in JIRA. The count will be linked to your JIRA site.
  * Cache  (Optional) The macro maintains a cache of the issues which result from the JIRA query. If the 'cache' parameter is set to 'off', the relevant part of the cache is cleared each time the macro is reloaded. (The value 'false' also works and has the same effect as 'off'.)
  * Height  (Optional) The height in pixels of the table displaying the issues.
  * RenderMode  (Optional) If the value is 'dynamic', the JIRA Issues macro offers an interactive display.
  * Title  (Optional) You can customise the title text at the top of the issues table with this parameter. For instance, setting the title to 'Bugs-to-fix' will replace the default 'JIRA Issues' text. This can help provide more context to the list of issues displayed.
  * Width  (Optional) The width of the table displaying the issues. Can be entered as a percentage (%) or in pixels (px).

  See: <https://confluence.atlassian.com/doc/jira-issues-macro-139380.html>

* template: `ac:emoticon` to include emoticons. Parameters:
  * Name: select emoticon
    * smile
    * sad
    * cheeky
    * laugh
    * wink
    * thumbs-up
    * thumbs-down
    * information
    * tick
    * cross
    * warning
    * plus
    * minus
    * question
    * light-on
    * light-off
    * yellow-star
    * red-star
    * green-star
    * blue-star

  See: <https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html>

* template: `ac:youtube` to include YouTube Widget. Parameters:
  * URL: YouTube video endpoint
  * Width: Width in px. Defaults to "640px"
  * Height: Height in px. Defaults to "360px"

  See: <https://confluence.atlassian.com/doc/widget-connector-macro-171180449.html#WidgetConnectorMacro-YouTube>

* template: `ac:children` to include Children Display macro
  * Reverse (Reverse Sort): Use with the `Sort Children By` parameter. When set, the sort order changes from ascending to descending.
    * `true`
    * `false` (Default)
  * Sort (Sort Children By):
    * `creation` — to sort by content creation date
    * `title` — to sort alphabetically on title
    * `modified` — to sort of last modification date.
    * If not specified, manual sorting is used if manually ordered, otherwise alphabetical.
  * Style (Heading Style): Choose the style used to display descendants.
    * from `h1` to `h6`
    * If not specified, default style is applied.
  * Page (Parent Page):
    * `/` — to list the top-level pages of the current space, i.e. those without parents.
    * `pagename` — to list the children of the specified page.
    * `spacekey:pagename` — to list the children of the specified page in the specified space.
    * If not specified, the current page is used.
  * Excerpt (Include Excerpts): Allows you to include a short excerpt under each page in the list.
    * `none` - no excerpt will be displayed. (Default)
    * `simple` - displays the first line of text contained in an Excerpt macro any of the returned pages. If there is not an Excerpt macro on the page, nothing will be shown.
    * `rich content` - displays the contents of an Excerpt macro, or if there is not an Excerpt macro on the page, the first part of the page content, including formatted text, images and some macros.
  * First (Number of Children): Restrict the number of child pages that are displayed at the top level.
    * If not specified, no limit is applied.
  * Depth (Depth of Descendants): Enter a number to specify the depth of descendants to display. For example, if the value is 2, the macro will display 2 levels of child pages. This setting has no effect if `Show Descendants` is enabled.
    * If not specified, no limit is applied.
  * All (Show Descendants): Choose whether to display all the parent page's descendants.
    * `true`
    * `false` (Default)

  See: <https://confluence.atlassian.com/doc/children-display-macro-139501.html>

* template: `ac:iframe` to include iframe macro (cloud only)
  * URL: URL to the iframe.
  * Frameborder: Choose whether to draw a border around content in the iframe.
    * `show` (Default)
    * `hide`
  * Width: Width in px. Defaults to "640px"
  * Height: Height in px. Defaults to "360px"
  * Scrolling: Allow or prevent scrolling in the iframe to see additional content.
    * `yes`
    * `no`
    * `auto` (Default)
  * Align: Align the iframe to the left or right of the page.
    * `left` (Default)
    * `right`

  See: <https://support.atlassian.com/confluence-cloud/docs/insert-the-iframe-macro>

* template: `ac:blog-posts`to include blog-posts
  * Content: How much content will be shown
    * titles (default)
    * excerpts
    * entire
  * Time: Specify how much back in time Confluence should look for blog posts (default: unlimited)
  * Label: Restrict to blog posts with specific labels
  * Author: Restrict to blog posts by specific authors
  * Spaces: Restrict to blog posts in specific spaces
  * Max: Maximum number of blog posts shown (default: 15)
  * Sort: Sorting posts by
    * title
    * creation (default)
    * modified
  * Reverse: Reverses the Sort parameter from oldest to newest (default: false)

  See: <https://confluence.atlassian.com/doc/blog-posts-macro-139470.html>

* template: `ac:include` to include a page
  * Page: the page to be included
  * Space: the space the page is in (optional, otherwise same space)

* template: `ac:excerpt-include` to include the excerpt from another page
  * Page: the page the excerpt should be included from
  * Name: The specific identifier for the excerpt, allowing multiple Excerpt macros on one page to be referenced individually. If not provided, the first excerpt from the page will be used (optional, cloud only)
  * NoPanel: Determines whether Confluence will display a panel around the excerpted content (optional, default: false)

* template: `ac:excerpt` to create an excerpt and include it in the page
  * Excerpt: The text you want to include
  * Name: Allows you to identify this macro so that you can add multiple Excerpt macros to one page and use a specific one on another page using the Excerpt Include macro (optional, cloud only)
  * OutputType: Determines whether the content of the Excerpt macro body is displayed on a new line or inline (optional, options: "BLOCK" or "INLINE", default: BLOCK)
  * Hidden: Hide the excerpt content (optional, default: false)

* template: `ac:anchor` to set an anchor inside a page
  * Anchor: Text for the anchor

* template: `ac:expand` to display an expandable/collapsible section of text on your page
  * Title: Defines the text next to the expand/collapse icon.
  * Body: The Text that it is expanded to.

* template: `ac:profile` to display a short summary of a given Confluence user's profile.
  * Name: The username of the Confluence user whose profile summary you wish to show.

* template: `ac:contentbylabel` to display a list of pages, blog posts or attachments that have particular labels
  * CQL: The CQL query to discover the content

* template: `ac:detailssummary` to show summary information from one page on a another page
  * Headings: Column headings to show
  * FirstColumn: Name of the Title Column
  * CQL: The CQL query to discover the pages
  * SortBy: Sort by a specific column heading

* template: `ac:details` to create page properties
  * Body: Must contain a table with two rows, the table headings are used as property key. The table content is the value.

* template: `ac:panel` to display a block of text within a customisable panel
  * Title: Panel title (optional)
  * Body: Body text of the panel
  * BGColor: Background Color
  * TitleBGColor: Background color of the title bar
  * TitleColor: Text color of the title
  * BorderStyle: Style of the panel's border
  * BorderColor: Color of the panel's border

* template `ac:recently-updated` to display a list of most recently changed content
  * Spaces: List of Spaces to watch (optional, default is current Space)
  * ShowProfilePic: Show profile picture of editor
  * Max: Maximum number of changes
  * Types: Include these content types only (comments, blogposts, pages)
  * Theme: Apperance of the macro (concise, social, sidebar)
  * HideHeading: Determines whether the macro hides or displays the text 'Recently Updated' as a title above the list of content
  * Labels: Filter the results by label. The macro will display only the pages etc which are tagged with the label(s) you specify here.

* template: `ac:pagetreesearch` to add a search box to your Confluence page.
  * Root: Name of the root page whose hierarchy of pages will be searched by this macro. If this not specified, the root page is the current page.

* template: `ac:column` To be used with the section macro to define the columns in a page.
  * Width: Width of the column
  * Body: The content of the column, rich text as in `ac:box` and `ac:panel`: Markdown and storage-format markup are rendered, not shown as text

* template: `ac:multimedia` to embedd an attached video, animation or other multimedia files in a Confluence page
  * Name: Name of the file
  * Width: Width of the video (optional)
  * AutoPlay: Start playing the file on page load (default: false)

* template `ac:view-file`
  * Name: Name of the file
  * Height: height of the view

* macro `@{...}` to mention user by name specified in the braces.

## Use cases

### Insert Disclaimer

This should be in **disclaimer.md**.

```markdown
**NOTE**: this document is generated, do not edit manually.
```

Add this to your **article.md**.

```markdown
<!-- Space: TEST -->
<!-- Title: My Article -->

<!-- Include: disclaimer.md -->

This is my article.
```

To put a disclaimer on every page without touching the documents, use
[`--page-header`](publishing.md#a-header-on-every-page) instead.

### Insert Status Badge

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

```markdown
<!-- Space: TEST -->
<!-- Title: TODO List -->

<!-- Macro: MYJIRA-\d+
     Template: ac:jira:ticket
     Ticket: ${0} -->

See task MYJIRA-123.
```
