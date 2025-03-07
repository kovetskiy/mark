package mark

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/stdlib"
	"github.com/stretchr/testify/assert"
)

func loadData(t *testing.T, filename, variant string) ([]byte, string, []byte) {
	t.Helper()
	basename := filepath.Base(filename)
	testname := strings.TrimSuffix(basename, ".md")
	htmlname := filepath.Join(filepath.Dir(filename), testname+variant+".html")

	markdown, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	html, err := os.ReadFile(htmlname)
	if err != nil {
		panic(err)
	}

	return markdown, htmlname, html
}

func TestCompileMarkdown(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	test := assert.New(t)

	testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		markdown, htmlname, html := loadData(t, filename, "")
		actual, _ := CompileMarkdown(markdown, lib, filename, "", 1.0, false, false)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
	}
}

func TestCompileMarkdownDropH1(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	test := assert.New(t)

        testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		var variant string
		switch filename {
		case "testdata/quotes.md", "testdata/header.md":
			variant = "-droph1"
		default:
			variant = ""
		}
		markdown, htmlname, html := loadData(t, filename, variant)
		actual, _ := CompileMarkdown(markdown, lib, filename, "", 1.0, true, false)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
	}
}

func TestCompileMarkdownStripNewlines(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	test := assert.New(t)

        testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		var variant string
                switch filename {
                case "testdata/quotes.md", "testdata/codes.md", "testdata/newlines.md", "testdata/macro-include.md":
                        variant = "-stripnewlines"
                default:
                        variant = ""
                }

		markdown, htmlname, html := loadData(t, filename, variant)
		actual, _ := CompileMarkdown(markdown, lib, filename, "", 1.0, false, true)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
	}
}

func TestMarkBinaryContinueOnError(t *testing.T) {
	var batchTestsDir string
	var markExePath string

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	if filepath.Base(wd) == "markdown" { // when running individual test, "go test -v ./markdown -run TestMarkBinaryContinueOnError" - move up to root project directory
		wd = filepath.Dir(wd)
	}

	batchTestsDir = filepath.Join(wd, "testdata/batch-tests")

	markExePath = filepath.Join(wd, "mark-temp")
	if runtime.GOOS == "windows" {
		markExePath += ".exe" // add .exe extension on Windows
	}

	t.Log("Building temporary mark executable...")
	buildCmd := exec.Command("go", "build", "-o", markExePath)

	var buildOutput bytes.Buffer
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build mark executable: %v\nOutput: %s", err, buildOutput.String())
	}

	if _, err := os.Stat(markExePath); err != nil {
		t.Fatalf("Test executable not found at %s: %v", markExePath, err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(markExePath, 0755); err != nil {
			t.Fatalf("Failed to make test executable executable: %v", err)
		}
	}
	defer os.Remove(markExePath) // remove created temporary executable at end of test

	t.Logf("Using temporary executable: %s", markExePath)
	t.Logf("Using batch tests directory: %s", batchTestsDir)

	filePath := filepath.Join(batchTestsDir, "*.md")
	cmd := exec.Command(markExePath, "--compile-only", "--continue-on-error", "-files", filePath)

	t.Logf("Using file pattern: %s", filePath)
	t.Logf("Command: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Log("Running command...")
	
	err = cmd.Run()
	if err != nil {
		t.Logf("Command exited with error: %v", err)
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("Failed to run mark binary: %v", err)
		}
	}

	combinedOutput := stdout.String() + stderr.String()
	var errorLines []string
	processedFiles := 0

	t.Log("Complete output:")
	t.Log(combinedOutput)
    
	scanner := bufio.NewScanner(strings.NewReader(combinedOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "ERROR") {
			errorLines = append(errorLines, line)
		}
		if strings.Contains(line, "processing") {
			processedFiles++;
		}
	}

	test := assert.New(t)
	test.EqualValues(3, len(errorLines))
	test.Contains(errorLines[0], "ERROR")
	test.Contains(errorLines[1], "ERROR")
	test.Contains(errorLines[2], "ERROR")
}