# Mark

Mark — tool for syncing your markdown documentation with Atlassian Confluence
pages.

This is very usable if you store documentation to your orthodox software in git
repository and don't want to do a handjob with updating Confluence page using
fucking tinymce wysiwyg enterprise core editor.

You can store a user credentials in the configuration file, which should be
located in ~/.config/mark with following format:

```toml
username = "smith"
password = "matrixishere"
base_url = "http://confluence.local"
```

Mark understands extended file format, which, still being valid markdown,
contains several metadata headers, which can be used to locate page inside
Confluence instance and update it accordingly.

File in extended format should follow specification
```markdown
<!-- Space: <space key> -->
<!-- Parent: <parent 1> -->
<!-- Parent: <parent 2> -->
<!-- Title: <title> -->

<page contents>
```

There can be any number of 'Parent' headers, if mark can't find specified
parent by title, it will be created.

Also, optional following headers are supported:

```markdown
<!-- Layout: (article|plain) -->
```

* (default) article: content will be put in narrow column for ease of
  reading;
* plain: content will fill all page;

Mark supports Go templates, which can be includes into article by using path
to the template relative to current working dir, e.g.:

```markdown
<!-- Include: <path> -->
```

Templates may accept configuration data in YAML format which immediately
follows include tag:

```markdown
<!-- Include: <path>
     <yaml-data> -->
```

Mark also supports macro definitions, which are defined as regexps which will
be replaced with specified template:

```markdown
<!-- Macro: <regexp>
     Template: <path>
     <yaml-data> -->
```

Capture groups can be defined in the macro's `<regexp>` which can be later
referenced in the `<yaml-data>` using `${<number>}` syntax.

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

### Insert Disclamer

**disclamer.md**

```markdown
**NOTE**: this document is generated, do not edit manually.
```

**article.md**
```markdown
<!-- Space: TEST -->
<!-- Title: My Article -->

<!-- Include: disclamer.md -->

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

## Insert Jira Ticket

**article.md**

```markdown
<!-- Space: TEST -->
<!-- Title: TODO List -->

<!-- Macro: MYJIRA-\d+
     Template: ac:jira:ticket
     Ticket: ${0} -->

See task MYJIRA-123.
```

## Usage

```
mark [options] [-u <username>] [-p <password>] [-k] [-l <url>] -f <file>
mark [options] [-u <username>] [-p <password>] [-k] [-n] -c <file>
mark -v | --version
mark -h | --help
```

- `-u <username>` — Use specified username for updating Confluence page.
- `-p <password>` — Use specified password for updating Confluence page.
- `-l <url>` — Edit specified Confluence page.
    If -l is not specified, file should contain metadata (see above).
- `-f <file>` — Use specified markdown file for converting to html.
- `-c <file>` — Specify configuration file which should be used for reading
    Confluence page URL and markdown file path.
- `-k` — Lock page editing to current user only to prevent accidental
    manual edits over Confluence Web UI.
- `--dry-run` — Show resulting HTML and don't update Confluence page content.
- `--trace` — Enable trace logs.
- `-v | --version`  — Show version.
- `-h | --help` — Show help screen and call 911.
