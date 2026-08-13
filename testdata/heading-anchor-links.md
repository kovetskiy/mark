# Anchor Links

Headings become ids that keep their capitals and punctuation, while the links
authors write use the lowercase-and-hyphens slug every other Markdown tool
produces. Both ends have to end up naming the same thing.

## My Heading

[slug style](#my-heading) and [exact style](#My-Heading).

## API/v2 Guide

The conventions disagree about which characters survive at all, so matching is
on the letters and digits: [dropped punctuation](#apiv2-guide).

## Release Notes

## Release.Notes

Two headings whose ids differ only in punctuation that matching drops are one
key, so [this](#release-notes) is left exactly as written rather than guessed
at.

[not an anchor](https://example.com/#fragment) is untouched, and so is
[an unknown target](#nothing-here).
