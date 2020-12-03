# Mark

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

* template `ac:jira:ticket` to include JIRA ticket link. Parameters:
  - Ticket: Jira ticket number like BUGS-123.

  See: https://confluence.atlassian.com/conf59/status-macro-792499207.html

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
- `-l <url>` — Edit specified Confluence page.
    If -l is not specified, file should contain metadata (see above).
- `-b <url>` or `--base-url <url>` – Base URL for Confluence.
    Alternative option for base_url config field.
- `-f <file>` — Use specified markdown file for converting to html.
- `-c <file>` — Specify configuration file which should be used for reading
    Confluence page URL and markdown file path.
- `-k` — Lock page editing to current user only to prevent accidental
    manual edits over Confluence Web UI.
- `--drop-h1` – Don't include H1 headings in Confluence output.
- `--dry-run` — Show resulting HTML and don't update Confluence page content.
- `--trace` — Enable trace logs.
- `-v | --version` — Show version.
- `-h | --help` — Show help screen and call 911.

You can store user credentials in the configuration file, which should be
located in ~/.config/mark with the following format (TOML):

```toml
username = "smith"
password = "matrixishere"
# If you are using Confluence Cloud add the /wiki suffix to base_url
base_url = "http://confluence.local"
```

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
