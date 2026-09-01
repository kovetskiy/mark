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

## 概要

A heading with no ASCII in it at all still has to become an id made of its own
characters rather than the literal word "heading": [the summary](#概要).

## 詳細

A second such heading is a heading of its own, not the numbered fallback that
one emptied id forces on the next.

## Heading with [link](https://example.com) and code

An id is built from what a reader sees, so the link's label counts and its URL
does not: [jump](#heading-with-link-and-code).
