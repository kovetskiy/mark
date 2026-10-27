package mark_test

import (
	"context"
	"testing"

	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
)

func TestGHAlertsBecomeMacros(t *testing.T) {
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
			name:        "Blockquote with info marker",
			markdown:    "> info: This is info marker syntax.",
			expectMacro: true,
			expectClean: false,
			description: "info: syntax should be converted to Confluence info macro",
		},
		{
			name:        "Regular blockquote",
			markdown:    "> This is just a regular blockquote.",
			expectMacro: false,
			expectClean: false,
			description: "Regular blockquotes should remain as HTML blockquote elements",
		},
	}

	stdlib, err := stdlib.New(context.Background(), nil)
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

			result, attachments, err := mark.CompileMarkdown([]byte(tc.markdown), stdlib, "/test", cfg)
			assert.NoError(t, err)

			assert.NotEmpty(t, result)
			assert.Empty(t, attachments, "Should have no attachments")

			if tc.expectMacro {
				assert.Contains(t, result, "structured-macro", "Should produce a Confluence macro")
			} else {
				assert.Contains(t, result, "<blockquote>", "Regular blockquote should use HTML blockquote")
			}
			if tc.expectClean {
				assert.NotContains(t, result, "[!", "Should remove GitHub Alert syntax markers")
			}
		})
	}
}
