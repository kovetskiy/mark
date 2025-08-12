package mark_test

import (
	"testing"

	mark "github.com/kovetskiy/mark/markdown"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/kovetskiy/mark/types"
	"github.com/stretchr/testify/assert"
)

func TestGHAlertsTransformerVsLegacyRenderer(t *testing.T) {
	testCases := []struct {
		name        string
		markdown    string
		expectMacro bool
		expectClean bool // Whether the [!TYPE] syntax should be cleaned up
		description string
	}{
		{
			name:        "GitHub Alert NOTE",
			markdown:    "> [!NOTE]\n> This is a test note.",
			expectMacro: true,
			expectClean: true,
			description: "GitHub Alert [!NOTE] syntax should be converted to Confluence info macro",
		},
		{
			name:        "GitHub Alert TIP",
			markdown:    "> [!TIP]\n> This is a helpful tip.",
			expectMacro: true,
			expectClean: true,
			description: "GitHub Alert [!TIP] syntax should be converted to Confluence tip macro",
		},
		{
			name:        "GitHub Alert WARNING",
			markdown:    "> [!WARNING]\n> This is a warning message.",
			expectMacro: true,
			expectClean: true,
			description: "GitHub Alert [!WARNING] syntax should be converted to Confluence note macro",
		},
		{
			name:        "GitHub Alert CAUTION",
			markdown:    "> [!CAUTION]\n> Be very careful here.",
			expectMacro: true,
			expectClean: true,
			description: "GitHub Alert [!CAUTION] syntax should be converted to Confluence warning macro",
		},
		{
			name:        "GitHub Alert IMPORTANT",
			markdown:    "> [!IMPORTANT]\n> This is very important.",
			expectMacro: true,
			expectClean: true,
			description: "GitHub Alert [!IMPORTANT] syntax should be converted to Confluence info macro",
		},
		{
			name:        "Legacy blockquote with info",
			markdown:    "> info: This is legacy info syntax.",
			expectMacro: true,
			expectClean: false,
			description: "Legacy info: syntax should be converted to Confluence info macro",
		},
		{
			name:        "Regular blockquote",
			markdown:    "> This is just a regular blockquote.",
			expectMacro: false,
			expectClean: false,
			description: "Regular blockquotes should remain as HTML blockquote elements",
		},
	}

	stdlib, err := stdlib.New(nil)
	if err != nil {
		t.Fatalf("Failed to create stdlib: %v", err)
	}

	cfg := types.MarkConfig{
		Features:      []string{},
		StripNewlines: false,
		DropFirstH1:   false,
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Testing: %s", tc.description)

			// Test with GitHub Alerts transformer (primary approach)
			transformerResult, transformerAttachments := mark.CompileMarkdown([]byte(tc.markdown), stdlib, "/test", cfg)

			// Test with legacy renderer
			legacyResult, legacyAttachments := mark.CompileMarkdownLegacy([]byte(tc.markdown), stdlib, "/test", cfg)

			// Basic checks
			assert.NotEmpty(t, transformerResult, "Transformer result should not be empty")
			assert.NotEmpty(t, legacyResult, "Legacy result should not be empty")
			assert.Empty(t, transformerAttachments, "Should have no attachments")
			assert.Empty(t, legacyAttachments, "Should have no attachments")

			// Check for Confluence macro presence
			if tc.expectMacro {
				assert.Contains(t, transformerResult, "structured-macro", "Transformer should produce Confluence macro")
				// Legacy renderer should NOT handle GitHub Alert syntax - it should treat as plain blockquote
				if tc.expectClean {
					// This is a GitHub Alert case - legacy should produce blockquote, transformer should produce macro
					assert.Contains(t, legacyResult, "<blockquote>", "Legacy renderer should treat GitHub Alerts as regular blockquotes")
				} else {
					// This is a legacy syntax case (like "info:") - both should produce macro
					assert.Contains(t, legacyResult, "structured-macro", "Legacy renderer should produce Confluence macro for legacy syntax")
				}
			} else {
				assert.Contains(t, transformerResult, "<blockquote>", "Regular blockquote should use HTML blockquote")
				assert.Contains(t, legacyResult, "<blockquote>", "Regular blockquote should use HTML blockquote")
			} // Check for GitHub Alert syntax cleanup (only for transformer with GitHub Alert syntax)
			if tc.expectClean {
				// Transformer should clean up the [!TYPE] syntax
				assert.NotContains(t, transformerResult, "[!", "Transformer should remove GitHub Alert syntax markers")

				// Legacy renderer might not clean it up (depending on implementation)
				// We'll just log what it produces for comparison
				t.Logf("Transformer result: %s", transformerResult)
				t.Logf("Legacy result: %s", legacyResult)
			} else {
				// For non-GitHub Alert cases, both should behave similarly
				t.Logf("Transformer result: %s", transformerResult)
				t.Logf("Legacy result: %s", legacyResult)
			}
		})
	}
}

func TestBasicTransformerFunctionality(t *testing.T) {
	testMarkdown := "> [!NOTE]\n> This is a test note."

	stdlib, err := stdlib.New(nil)
	if err != nil {
		t.Fatalf("Failed to create stdlib: %v", err)
	}

	cfg := types.MarkConfig{
		Features:      []string{},
		StripNewlines: false,
		DropFirstH1:   false,
	}

	result, attachments := mark.CompileMarkdown([]byte(testMarkdown), stdlib, "/test", cfg)

	// Basic checks
	assert.NotEmpty(t, result)
	assert.Empty(t, attachments)
	assert.Contains(t, result, "structured-macro")

	// This test should now pass because we fixed the transformer
	assert.NotContains(t, result, "[!NOTE]", "The GitHub Alert syntax should be cleaned up")

	t.Logf("Transformer result: %s", result)
}

// TestCompatibilityWithExistingFeatures tests that the transformer approach is fully compatible
// with existing non-blockquote functionality from the original markdown tests
func TestCompatibilityWithExistingFeatures(t *testing.T) {
	testCases := []struct {
		name        string
		markdown    string
		config      types.MarkConfig
		description string
	}{
		{
			name: "Headers Basic",
			markdown: `# Header 1
## Header 2
### Header 3`,
			config: types.MarkConfig{
				Features:      []string{},
				StripNewlines: false,
				DropFirstH1:   false,
			},
			description: "Basic header rendering should be identical",
		},
		{
			name: "Headers with DropFirstH1",
			markdown: `# Header 1
## Header 2
### Header 3`,
			config: types.MarkConfig{
				Features:      []string{},
				StripNewlines: false,
				DropFirstH1:   true,
			},
			description: "Header rendering with DropFirstH1 should be identical",
		},
		{
			name:     "Code Blocks",
			markdown: "`inline code`\n\n```bash\necho \"hello\"\n```",
			config: types.MarkConfig{
				Features:      []string{},
				StripNewlines: false,
				DropFirstH1:   false,
			},
			description: "Code block rendering should be identical",
		},
		{
			name: "Links and Images",
			markdown: `[Link](https://example.com)
![Image](test.png)
[Page Link](ac:Page)`,
			config: types.MarkConfig{
				Features:      []string{},
				StripNewlines: false,
				DropFirstH1:   false,
			},
			description: "Links and images should be rendered identically",
		},
		{
			name: "Tables",
			markdown: `| Header 1 | Header 2 |
|----------|----------|
| Row 1    | Row 2    |`,
			config: types.MarkConfig{
				Features:      []string{},
				StripNewlines: false,
				DropFirstH1:   false,
			},
			description: "Table rendering should be identical",
		},
		{
			name: "Mixed Content",
			markdown: `# Title

Some **bold** and *italic* text.

- List item 1
- List item 2

` + "`inline code`" + ` and:

` + "```javascript\nconsole.log(\"test\");\n```" + `

[Link](https://example.com)`,
			config: types.MarkConfig{
				Features:      []string{},
				StripNewlines: false,
				DropFirstH1:   false,
			},
			description: "Mixed content should be rendered identically",
		},
		{
			name: "Strip Newlines",
			markdown: `Line 1

Line 2


Line 3`,
			config: types.MarkConfig{
				Features:      []string{},
				StripNewlines: true,
				DropFirstH1:   false,
			},
			description: "StripNewlines functionality should work identically",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Testing: %s", tc.description)

			stdlib, err := stdlib.New(nil)
			if err != nil {
				t.Fatalf("Failed to create stdlib: %v", err)
			}

			// Test with GitHub Alerts transformer (primary approach)
			transformerResult, transformerAttachments := mark.CompileMarkdown([]byte(tc.markdown), stdlib, "/test", tc.config)

			// Test with legacy renderer (original approach)
			legacyResult, legacyAttachments := mark.CompileMarkdownLegacy([]byte(tc.markdown), stdlib, "/test", tc.config)

			// Basic checks
			assert.NotEmpty(t, transformerResult, "Transformer result should not be empty")
			assert.NotEmpty(t, legacyResult, "Legacy result should not be empty")
			assert.Equal(t, len(transformerAttachments), len(legacyAttachments), "Attachment counts should match")

			// The key compatibility test: results should be identical for non-blockquote content
			if transformerResult != legacyResult {
				t.Errorf("COMPATIBILITY ISSUE: Results differ for %s\n"+
					"Transformer result:\n%s\n\n"+
					"Legacy result:\n%s\n\n"+
					"Diff (transformer vs legacy):",
					tc.name, transformerResult, legacyResult)

				// Log the differences for debugging
				t.Logf("Transformer length: %d", len(transformerResult))
				t.Logf("Legacy length: %d", len(legacyResult))

				// Character-by-character comparison for debugging
				for i := 0; i < len(transformerResult) && i < len(legacyResult); i++ {
					if transformerResult[i] != legacyResult[i] {
						t.Logf("First difference at position %d: transformer='%c'(%d) vs legacy='%c'(%d)",
							i, transformerResult[i], transformerResult[i], legacyResult[i], legacyResult[i])
						break
					}
				}
			} else {
				t.Logf("✅ Perfect compatibility for %s", tc.name)
			}
		})
	}
}
