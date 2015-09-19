# Mark

Mark it's tool for syncing your markdown documentation with Atlassian Confluence pages.

This is very usable if you store documentation to your orthodox software in git
repository and don't want to do a handjob with updating Confluence page using
fucking tinymce wysiwyg enterprise core editor.

You can store a user credentials in the configuration file, which should be
located in ~/.config/mark with following format:
```
username = "smith"
password = "matrixishere"
```

Mark can read Confluence page URL and markdown file path from another specified
configuration file, which you can specify using -c <file> flag. It is very
usable for git hooks. That file should have following format:
```toml
url = "http://confluence.local/pages/viewpage.action?pageId=123456"
file = "docs/README.md"
```

## Usage:
```
mark [-u <username>] [-p <password>] -l <url> -f <file>
mark [-u <username>] [-p <password>] -c <file>
mark -v | --version
mark -h | --help
```

- `-u <username>` - Use specified username for updating Confluence page.
- `-p <password>` - Use specified password for updating Confluence page.
- `-l <url>` - Edit specified Confluence page.
- `-f <file>` - Use specified markdown file for converting to html.
- `-c <file>` - Specify configuration file which should be used for reading Confluence page URL and markdown file path.
- `-v | --version`  - Show version.
- `-h | --help` - Show help screen and call 911.
