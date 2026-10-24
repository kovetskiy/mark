# Collapsed Admonitions

## Collapsed

??? note "Click to expand"
    * Hidden bullet 1
    * Hidden bullet 2

??? tip
    No title, so the type names the expand.

## Open by default

???+ warning "Open"
    Reads like the ordinary admonition.

## Closing run

??? info "Closed"
    Inside.
???

After the closing run.

## Nested

!!! note "Outer"
    Text.

    ??? tip "Inner"
        Hidden.

## Escaping

??? note "A & B <C>"
    Body.

## Attributes and quotes

??? note "He said \"hi\" & it's" {.wide #x}
    Body.

## Nested collapsed

??? info "Outer"
    Text.

    ??? tip "Inner"
        Hidden.

## Prose stays text

??? what is this
??? Really?
Still a paragraph.
