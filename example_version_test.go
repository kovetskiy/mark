package mark_test

import (
	"fmt"

	"github.com/kovetskiy/mark/v17"
)

// ExampleVersion names the release of mark a program was built with.
func ExampleVersion() {
	version := mark.Version()
	if version == "" {
		version = "a build of mark that is not a release"
	}

	fmt.Println("publishing with", version)
}
