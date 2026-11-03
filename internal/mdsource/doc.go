// Package mdsource answers questions about a Markdown document's raw bytes
// that have to be settled before goldmark builds the page: which spans are
// code, which regions are marked as not for Confluence, and what shape the
// bytes must be in for every parser to read them alike.
//
// It imports none of mark's other packages, so the parsers, macros and
// includes that need it do not pull in header parsing with it.
package mdsource
