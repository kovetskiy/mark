# Mark

<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-24-orange.svg?style=flat-square)](#contributors-)
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

Templates can accept configuration data in YAML format which immediately
follows the `Include` tag:

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

<!-- Macro: :box:(.+):(.*):(.+):
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

### Go Get

```bash
go get -v github.com/kovetskiy/mark
```

### Releases

[Download a release from the Releases page](https://github.com/kovetskiy/mark/releases)

### Docker

```bash
$ docker run --rm -i kovetskiy/mark:latest mark <params>
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
- `--drop-h1` – Don't include H1 headings in Confluence output.
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
  </tr>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
