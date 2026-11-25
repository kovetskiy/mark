package mark_test

import (
	"fmt"

	"github.com/kovetskiy/mark/v17"
)

// ExampleVersion names the version of mark a program was built with.
func ExampleVersion() {
	version := mark.Version()
	if version == "" {
		version = "a local copy of mark"
	}

	fmt.Println("publishing with", version)
}
