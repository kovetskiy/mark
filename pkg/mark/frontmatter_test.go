package mark

import (
	"testing"
	"io/ioutil"
	"path/filepath"
	"strings"
	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/stretchr/testify/assert"
)

// const (
// 	NL = "\n"
// )

// func text(lines ...string) string {
// 	return strings.Join(lines, "\n")
// }

func TestCompileMarkdownWithFrontMatter(t *testing.T) {
	test := assert.New(t)

	testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		basename := filepath.Base(filename)
		testname := strings.TrimSuffix(basename, ".md")
		htmlname := filepath.Join(filepath.Dir(filename), testname+".html")

		markdown, err := ioutil.ReadFile(filename)
		if err != nil {
			panic(err)
		}
		html, err := ioutil.ReadFile(htmlname)
		if err != nil {
			panic(err)
		}

		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		actual := CompileMarkdown(markdown, lib)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
	}
}


// // TestHelloName calls greetings.Hello with a name, checking
// // for a valid return value.
// func TestHelloName(t *testing.T) {
// 	name := "Gladys"
// 	want := regexp.MustCompile(`\b`+name+`\b`)
// 	msg, err := Hello("Gladys")
// 	if !want.MatchString(msg) || err != nil {
// 		t.Fatalf(`Hello("Gladys") = %q, %v, want match for %#q, nil`, msg, err, want)
// 	}
// }

// // TestHelloEmpty calls greetings.Hello with an empty string,
// // checking for an error.
// func TestHelloEmpty(t *testing.T) {
// 	msg, err := Hello("")
// 	if msg != "" || err == nil {
// 		t.Fatalf(`Hello("") = %q, %v, want "", error`, msg, err)
// 	}
// }
