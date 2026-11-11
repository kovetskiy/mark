package mark_test

import (
	"fmt"

	"github.com/kovetskiy/mark/v17"
)

// ExampleImage writes the image a CI job publishes with, so that the headless
// Chrome it draws in is the one the linked release of mark was built for. A
// program linked against a pseudo-version or a local copy of mark, as this
// example is under go test, has no image to name and asks for one instead.
func ExampleImage() {
	image := mark.Image()
	if image == "" {
		fmt.Println("this build links no release of mark: name the image to run")

		return
	}

	fmt.Printf("image: %s\n", image)
	// Output: this build links no release of mark: name the image to run
}

// ExampleVersion names the release of mark a program was built with.
func ExampleVersion() {
	version := mark.Version()
	if version == "" {
		version = "a build of mark that is not a release"
	}

	fmt.Println("publishing with", version)
	// Output: publishing with a build of mark that is not a release
}
