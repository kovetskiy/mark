package mark_test

import (
	"fmt"

	"github.com/kovetskiy/mark/v17"
)

// ExampleImage writes the image a CI job publishes with, so that the headless
// Chrome it draws in is the one the linked release of mark was built for. A
// program linked against a pseudo-version or a local copy of mark has no
// image to name and asks for one instead.
//
// The example has no output to check: it depends on the build that links
// mark, and go test of a module that requires a release of mark links that
// release.
func ExampleImage() {
	image := mark.Image()
	if image == "" {
		fmt.Println("this build links no release of mark: name the image to run")

		return
	}

	fmt.Printf("image: %s\n", image)
}

// ExampleVersion names the release of mark a program was built with.
func ExampleVersion() {
	version := mark.Version()
	if version == "" {
		version = "a build of mark that is not a release"
	}

	fmt.Println("publishing with", version)
}
