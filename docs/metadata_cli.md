# Confluence metadata commands

`mark metadata` provides commands for working with Confluence Cloud metadata.
It reuses Mark's connection and authentication settings.

## Listing labels

`mark metadata labels list` lists labels visible to the authenticated user.
Without a selector, it lists labels across the site.
Choose at most one selector per listing.
Selectors must be explicit command-line arguments; environment and configuration space settings are ignored.

| Command | Lists |
| --- | --- |
| `mark metadata labels list` | Site-wide labels visible to the authenticated user |
| `mark metadata labels list --space RD` | Labels used by content in space `RD` |
| `mark metadata labels list --space-id 123456` | Labels attached to the space itself |
| `mark metadata labels list --page-id 123456` | Labels attached to a page |
| `mark metadata labels list --blogpost-id 123456` | Labels attached to a blog post |
| `mark metadata labels list --attachment-id 123456` | Labels attached to an attachment |
| `mark metadata labels list --custom-content-id 123456` | Labels attached to a custom content item |

`--space` selects labels on a space's content; `--space-id` selects labels on the space object.

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
| `mark metadata statuses list --page-id 123456` | The page's assigned status, if any |

Space status suggestions can include unused statuses.
A page's assigned status can be custom.
Personal statuses are the user's creations, independent of statuses assigned to their pages.

## Listing output

The list commands show a table by default, with names, IDs, and label prefixes or status colors.
Empty listings display `No labels found.` or `No statuses found.`.

Use `--json` to write one JSON array to stdout instead:

```shell
mark metadata labels list --space RD --json
mark metadata statuses list --space RD --json
```

Labels contain `id` (string), `name`, and `prefix`.
Statuses contain `id` (integer), `name`, and `color`.
In JSON output, an empty listing or an unassigned page status produces `[]`.
