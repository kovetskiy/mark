package parser_test

import (
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/ast"
)

func TestConfluenceIDs_Generate(t *testing.T) {
	t.Run("Standard title with spaces", func(t *testing.T) {
		ids := cparser.NewConfluenceIDs()
		res := ids.Generate([]byte("Hello World"), ast.KindHeading)
		assert.Equal(t, "Hello-World", string(res))
	})

	t.Run("Title preserving slashes underscores and dots", func(t *testing.T) {
		ids := cparser.NewConfluenceIDs()
		res := ids.Generate([]byte("This/is some_Heading.yml"), ast.KindHeading)
		assert.Equal(t, "This/is-some_Heading.yml", string(res))
	})

	t.Run("Collision handling", func(t *testing.T) {
		ids := cparser.NewConfluenceIDs()
		res1 := ids.Generate([]byte("Section"), ast.KindHeading)
		res2 := ids.Generate([]byte("Section"), ast.KindHeading)
		res3 := ids.Generate([]byte("Section"), ast.KindHeading)

		assert.Equal(t, "Section", string(res1))
		assert.Equal(t, "Section-1", string(res2))
		assert.Equal(t, "Section-2", string(res3))
	})

	t.Run("Empty title fallback", func(t *testing.T) {
		ids := cparser.NewConfluenceIDs()
		resHeading := ids.Generate([]byte("!!!"), ast.KindHeading)
		resOther := ids.Generate([]byte("???"), ast.KindParagraph)

		assert.Equal(t, "heading", string(resHeading))
		assert.Equal(t, "id", string(resOther))
	})

	t.Run("Lazy initialization of nil map", func(t *testing.T) {
		var ids cparser.ConfluenceIDs
		res := ids.Generate([]byte("Test Header"), ast.KindHeading)
		assert.Equal(t, "Test-Header", string(res))

		ids.Put([]byte("ManualID"))
		resDup := ids.Generate([]byte("ManualID"), ast.KindHeading)
		assert.Equal(t, "ManualID-1", string(resDup))
	})
}
