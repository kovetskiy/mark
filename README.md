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

Mark can read Confluence page URL and markdown file path from another specified
configuration file, which you can specify using -c <file> flag. It is very
usable for git hooks. That file should have following format:
```toml
url = "http://confluence.local/pages/viewpage.action?pageId=123456"
file = "docs/README.md"
```

Mark understands extended file format, which, still being valid markdown,
contains several metadata headers, which can be used to locate page inside
Confluence instance and update it accordingly.

File in extended format should follow specification
```markdown
[]:# (X-Space: <space key>)
[]:# (X-Parent: <parent 1>)
[]:# (X-Parent: <parent 2>)
[]:# (X-Title: <title>)

<page contents>
```

There can be any number of 'X-Parent' headers, if mark can't find specified
parent by title, it will be created.

## Usage:
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

# changes by autonubil
- only updated changed pages
- upload local images as attachments