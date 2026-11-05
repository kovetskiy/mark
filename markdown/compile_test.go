package markdown

import (
	"context"
	"testing"

	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/require"
)

// compileDoc compiles a document with the stdlib templates loaded, which is
// what a test asserting on rendered storage format wants.
func compileDoc(t *testing.T, src string, features ...string) string {
	t.Helper()

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", Options{Features: features})
	require.NoError(t, err)

	return out
}
