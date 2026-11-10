package parser_test

import (
	"errors"
	"fmt"

	"github.com/kovetskiy/mark/v17/parser"
)

// A generator mentioning users by display names it does not control: the
// characters that would end the mention, or the table cell around it, are
// escaped, and mark mentions exactly the name given.
func ExampleFormatMention() {
	for _, name := range []string{"Jane Doe", "Ops {on call}", `CORP\jdoe`, "a|b"} {
		mention, err := parser.FormatMention(name)
		if err != nil {
			panic(err)
		}
		fmt.Printf("| %s |\n", mention)
	}

	_, err := parser.FormatMention("Jane\n# Heading")
	fmt.Println(errors.Is(err, parser.ErrUnrepresentable))

	// Output:
	// | @{Jane Doe} |
	// | @{Ops {on call\}} |
	// | @{CORP\\jdoe} |
	// | @{a\|b} |
	// true
}
