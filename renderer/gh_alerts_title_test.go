package renderer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGHAlertTitleIsAMacroParameter pins where an alert's name is published.
//
// It used to be synthesised into the body as a paragraph of its own, so
// Confluence drew it as the alert's first line of text rather than as its
// header -- unstyled, unaligned with the icon, and part of the content a
// reader editing the page could delete. The macro has a title parameter for
// exactly this and renders it as a header.
func TestGHAlertTitleIsAMacroParameter(t *testing.T) {
	tests := []struct {
		alert string
		macro string
		title string
	}{
		{"NOTE", "info", "Note"},
		{"TIP", "tip", "Tip"},
		{"IMPORTANT", "info", "Important"},
		{"WARNING", "note", "Warning"},
		{"CAUTION", "warning", "Caution"},
	}

	for _, tt := range tests {
		t.Run(tt.alert, func(t *testing.T) {
			actual := render(t, "> [!"+tt.alert+"]\n> The body.\n", ghAlertRenderers(), ghAlertParserOptions()...)
			assertWellFormed(t, actual)

			assert.Contains(t, actual,
				`<ac:structured-macro ac:name="`+tt.macro+`"><ac:parameter ac:name="icon">true</ac:parameter>`+
					`<ac:parameter ac:name="title">`+tt.title+`</ac:parameter><ac:rich-text-body>`)
			assert.NotContains(t, actual, "<p>"+tt.title+"</p>",
				"the title belongs in the macro header, not in its body")
			assert.Contains(t, actual, "The body.")
		})
	}
}

// TestGHAlertTitleDoesNotReachOtherQuotes covers the two blockquotes that are
// not alerts. Neither has a name to publish, so neither may grow a title.
func TestGHAlertTitleDoesNotReachOtherQuotes(t *testing.T) {
	for _, source := range []string{"> Warn: the old syntax.\n", "> Just a quotation.\n"} {
		t.Run(source, func(t *testing.T) {
			actual := render(t, source, ghAlertRenderers(), ghAlertParserOptions()...)
			assertWellFormed(t, actual)

			assert.NotContains(t, actual, `ac:name="title"`)
		})
	}
}
