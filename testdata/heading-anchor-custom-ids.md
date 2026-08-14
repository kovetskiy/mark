# Custom Anchor IDs

An author can name a heading's anchor with the `{#custom-id}` syntax every other
Markdown tool understands, rather than accepting the one Mark derives from the
heading text.

## My Heading {#custom-id}

The braces are consumed rather than rendered, so the heading reads as written
and the anchor is exactly what was asked for: [exact](#custom-id).

## Another Heading

A heading with no custom id keeps its derived anchor, and a link written in the
usual slug style still finds it: [slug](#another-heading).

## Release Notes {#rel}

Naming an anchor replaces the derived one rather than adding to it, so the slug
of the heading text no longer resolves: [by slug](#release-notes) is left as
written, while [by name](#rel) resolves. This is what other Markdown tools do
too -- a custom id is the id, not an alias.
