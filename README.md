# Mark

<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-26-orange.svg?style=flat-square)](#contributors-)
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
<!-- Sidebar: <h2>Test</h2> -->
```

Setting the sidebar creates a column on the right side.  You're able to add any valid HTML content. Adding this property sets the layout to `article`.

Mark supports Go templates, which can be included into article by using path
to the template relative to current working dir, e.g.:

```markdown
<!-- Include: <path> -->
```

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

You can collapse or have a title without language or any mix, but the language
must stay in the front _if it is given_:

    [<language>] ["collapse"] ["title" <your title>]

[Code Block Macro]: https://confluence.atlassian.com/doc/code-block-macro-139390.html

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
  - Width: Width in px. Defualts to "640px"
  - Height: Height in px. Defualts to "360px"

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
  - Width: Width in px. Defualts to "640px"
  - Height: Height in px. Defualts to "360px"
  - Scrolling: Allow or prevent scrolling in the iframe to see additional content.
      - `yes`
      - `no`
      - `auto` (Default)
  - Align: Align the iframe to the left or right of the page.
      - `left` (Default)
      - `right`

  See: https://support.atlassian.com/confluence-cloud/docs/insert-the-iframe-macro

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
mark [options] [-u <username>] [-p <password>] [-k] [-l <url>] -f <file>
mark [options] [-u <username>] [-p <password>] [-k] [-b <url>] -f <file>
mark [options] [-u <username>] [-p <password>] [--drop-h1] -f <file>
mark -v | --version
mark -h | --help
```

- `-u <username>` — Use specified username for updating Confluence page.
- `-p <password>` — Use specified password for updating Confluence page.
    Specify `-` as password to read password from stdin.
- `-l <url>` — Edit specified Confluence page.
    If -l is not specified, file should contain metadata (see above).
- `-b <url>` or `--base-url <url>` – Base URL for Confluence.
    Alternative option for `base_url` config field.
- `-f <file>` — Use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted).
- `-c <path>` or `--config <path>` — Specify a path to the configuration file.
- `-k` — Lock page editing to current user only to prevent accidental
    manual edits over Confluence Web UI.
- `--space <space>` - Use specified space key. If not specified space ley must be set in a page metadata.
- `--drop-h1` – Don't include H1 headings in Confluence output.
  This option corresponds to the `h1_drop` setting in the configuration file.
- `--title-from-h1` - Extract page title from a leading H1 heading. If no H1 heading on a page then title must be set in a page metadata.
  This option corresponds to the `h1_title` setting in the configuration file.
- `--dry-run` — Show resulting HTML and don't update Confluence page content.
- `--minor-edit` — Don't send notifications while updating Confluence page.
- `--trace` — Enable trace logs.
- `-v | --version` — Show version.
- `-h | --help` — Show help screen and call 911.

You can store user credentials in the configuration file, which should be
located in ~/.config/mark (or specified via `-c --config <path>`) with the following format (TOML):

```toml
username = "your-email"
password = "password-or-api-key-for-confluence-cloud"
# If you are using Confluence Cloud add the /wiki suffix to base_url
base_url = "http://confluence.local"
h1_title = true
h1_drop = true
```

**NOTE**: Labels aren't supported when using `minor-edit`!

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
  <tr>
    <td align="center"><a href="https://github.com/seletskiy"><img src="https://avatars.githubusercontent.com/u/674812?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Stanislav Seletskiy</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=seletskiy" title="Code">💻</a></td>
    <td align="center"><a href="https://klauer.dev/"><img src="https://avatars.githubusercontent.com/u/4735?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Nick Klauer</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=klauern" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/rofafor"><img src="https://avatars.githubusercontent.com/u/9297850?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Rolf Ahrenberg</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=rofafor" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/csoutherland"><img src="https://avatars.githubusercontent.com/u/840471?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Charles Southerland</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=csoutherland" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/snejus"><img src="https://avatars.githubusercontent.com/u/16212750?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Šarūnas Nejus</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=snejus" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/brnv"><img src="https://avatars.githubusercontent.com/u/1925213?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Alexey Baranov</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=brnv" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/princespaghetti"><img src="https://avatars.githubusercontent.com/u/2935312?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Anthony Barbieri</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=princespaghetti" title="Code">💻</a></td>
  </tr>
  <tr>
    <td align="center"><a href="https://github.com/dauc"><img src="https://avatars.githubusercontent.com/u/29129213?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Devin Auclair</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=dauc" title="Code">💻</a></td>
    <td align="center"><a href="https://gezimsejdiu.github.io/"><img src="https://avatars.githubusercontent.com/u/5259296?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Gezim Sejdiu</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=GezimSejdiu" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/jcavar"><img src="https://avatars.githubusercontent.com/u/3751289?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Josip Ćavar</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jcavar" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/Hi-Fi"><img src="https://avatars.githubusercontent.com/u/1499780?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Juho Saarinen</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Hi-Fi" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/lukiffer"><img src="https://avatars.githubusercontent.com/u/2278911?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Luke Fritz</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=lukiffer" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/MattyRad"><img src="https://avatars.githubusercontent.com/u/1143595?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Matt Radford</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=MattyRad" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/Planktonette"><img src="https://avatars.githubusercontent.com/u/5514719?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Planktonette</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Planktonette" title="Code">💻</a></td>
  </tr>
  <tr>
    <td align="center"><a href="http://www.stefanoteodorani.it/"><img src="https://avatars.githubusercontent.com/u/2573389?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Stefano Teodorani</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=teopost" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/tillepille"><img src="https://avatars.githubusercontent.com/u/16536696?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Tim Schrumpf</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=tillepille" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/tyler-copilot"><img src="https://avatars.githubusercontent.com/u/18539108?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Tyler Cole</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=tyler-copilot" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/elgreco247"><img src="https://avatars.githubusercontent.com/u/8968417?v=4?s=100" width="100px;" alt=""/><br /><sub><b>elgreco247</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=elgreco247" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/emead-indeed"><img src="https://avatars.githubusercontent.com/u/44018145?v=4?s=100" width="100px;" alt=""/><br /><sub><b>emead-indeed</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=emead-indeed" title="Code">💻</a></td>
    <td align="center"><a href="https://wbhegedus.me/"><img src="https://avatars.githubusercontent.com/u/11506822?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Will Hegedus</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=wbh1" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/carnei-ro"><img src="https://avatars.githubusercontent.com/u/42899277?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Leandro Carneiro</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=carnei-ro" title="Code">💻</a></td>
  </tr>
  <tr>
    <td align="center"><a href="https://github.com/beeme1mr"><img src="https://avatars.githubusercontent.com/u/682996?v=4?s=100" width="100px;" alt=""/><br /><sub><b>beeme1mr</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=beeme1mr" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/Taldrain"><img src="https://avatars.githubusercontent.com/u/1081600?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Taldrain</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Taldrain" title="Code">💻</a></td>
    <td align="center"><a href="http://www.devin.com.br/"><img src="https://avatars.githubusercontent.com/u/349457?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Hugo Cisneiros</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=eitchugo" title="Code">💻</a></td>
    <td align="center"><a href="https://github.com/jevfok"><img src="https://avatars.githubusercontent.com/u/54530686?v=4?s=100" width="100px;" alt=""/><br /><sub><b>jevfok</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jevfok" title="Code">💻</a></td>
    <td align="center"><a href="https://dev.to/mmiranda"><img src="https://avatars.githubusercontent.com/u/16670310?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Mateus Miranda</b></sub></a><br /><a href="#maintenance-mmiranda" title="Maintenance">🚧</a></td>
    <td align="center"><a href="https://github.com/Skeeve"><img src="https://avatars.githubusercontent.com/u/725404?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Skeeve</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Skeeve" title="Code">💻</a></td>
  </tr>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
