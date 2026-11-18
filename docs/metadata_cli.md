# Confluence metadata commands

`mark metadata` provides commands for working with Confluence Cloud metadata.
It reuses Mark's connection and authentication settings.

> [!WARNING]
> `mark metadata` is experimental. Its commands, flags, and output may change between releases.

## Listing labels

`mark metadata labels list` lists labels visible to the authenticated user.
Without a selector, it lists labels across the site.
Choose at most one selector per listing.
Selectors must be explicit command-line arguments; environment and configuration space settings are ignored.

| Command | Lists |
| --- | --- |
| `mark metadata labels list` | Site-wide labels visible to the authenticated user |
| `mark metadata labels list --in-space RD` | Labels used by content in space `RD` |
| `mark metadata labels list --space RD` | Labels attached to the space itself |
| `mark metadata labels list --page 123456` | Labels attached to a page |
| `mark metadata labels list --blogpost 123456` | Labels attached to a blog post |
| `mark metadata labels list --attachment 123456` | Labels attached to an attachment |
| `mark metadata labels list --custom-content 123456` | Labels attached to a custom content item |

`--in-space` selects labels used by content inside a space; `--space` selects labels attached to the space itself.
Each takes a space key.

`--page` and `--blogpost` accept an ID or a Confluence URL:

```shell
mark metadata labels list --page 'https://example.atlassian.net/wiki/spaces/RD/pages/123456/Title'
mark metadata labels list --blogpost 'https://example.atlassian.net/wiki/spaces/RD/blog/2026/11/18/123456/Title'
```

URLs can supply the base URL; a configured base URL must name the same instance.
Short links and `/display/SPACE/Title` links are supported through the same selectors.

`--attachment` accepts an attachment ID, a preview URL, or a download URL.
Preview URLs carry `preview=/PAGE_ID/ATTACHMENT_ID/FILENAME`.
Download URLs use `/wiki/download/attachments/PAGE_ID/FILENAME`.
Download URLs are resolved by filename within the parent content's attachments.
`--custom-content` accepts an ID or a URL ending in `/display/SPACE/customcontent/ID`.

The `PREFIX` column identifies the label namespace:

| Prefix | Meaning |
| --- | --- |
| `global` | Shared labels |
| `my` | Personal labels |
| `team` | Historical namespace used to group spaces into categories |

These namespaces are separate from the object or space selected by the command.

## Listing statuses

`mark metadata statuses list` lists content statuses from the selected source.
Choose one explicit command-line selector; without one, the command shows help.

| Command | Lists |
| --- | --- |
| `mark metadata statuses list --space RD` | Suggested statuses configured for space `RD` |
| `mark metadata statuses list --personal` | Custom statuses created by the authenticated user |
| `mark metadata statuses list --page 123456` | The page's assigned status, if any |

`--page` also accepts a Confluence URL, as for label listings.

Space status suggestions can include unused statuses.
A page's assigned status can be custom.
Personal statuses are the user's creations, independent of statuses assigned to their pages.

## Listing output

The list commands show a table by default, with names, IDs, and label prefixes or status colors.
Empty listings display `No labels found.` or `No statuses found.`.

Use `--json` to write one JSON array to stdout instead:

```shell
mark metadata labels list --in-space RD --json
mark metadata statuses list --space RD --json
```

Labels contain `id` (string), `name`, and `prefix`.
Statuses contain `id` (integer), `name`, and `color`.
In JSON output, an empty listing or an unassigned page status produces `[]`.
