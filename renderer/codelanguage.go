package renderer

import "strings"

// codeLanguageAliases maps the names a fence is commonly given to the name
// Confluence's code macro knows the language by. Confluence has no aliases of
// its own: Data Center fails to render a code macro whose language it does not
// know, and Cloud shows it unhighlighted.
//
// Cloud and Data Center spell some languages differently -- JavaScript is "js"
// on one and "javascript" on the other -- and compiling does not know which it
// is publishing to, so only two kinds of alias are here: those whose target
// both platforms share, and those whose target only Cloud has, where the alias
// would not work on Data Center either. The others are left as written.
//
// The names follow the Code Block macro's documentation and the table
// sphinx-contrib/confluencebuilder keeps in std/confluence.py.
var codeLanguageAliases = map[string]string{
	// Both platforms.
	"text":          "none",
	"txt":           "none",
	"plain":         "none",
	"plaintext":     "none",
	"raw":           "none",
	"as3":           "actionscript3",
	"c++":           "cpp",
	"cxx":           "cpp",
	"cc":            "cpp",
	"hpp":           "cpp",
	"cfc":           "coldfusion",
	"udiff":         "diff",
	"patch":         "diff",
	"pl":            "perl",
	"php3":          "php",
	"php4":          "php",
	"php5":          "php",
	"posh":          "powershell",
	"ps1":           "powershell",
	"psm1":          "powershell",
	"pwsh":          "powershell",
	"py":            "python",
	"py3":           "python",
	"python3":       "python",
	"bazel":         "python",
	"starlark":      "python",
	"rb":            "ruby",
	"sh":            "bash",
	"shell":         "bash",
	"zsh":           "bash",
	"ksh":           "bash",
	"console":       "bash",
	"shell-session": "bash",
	"shellsession":  "bash",
	"vbscript":      "vb",
	"visualbasic":   "vb",
	"yml":           "yaml",

	// Cloud only.
	"clj":        "clojure",
	"coffee":     "coffeescript",
	"cu":         "cuda",
	"dockerfile": "docker",
	"ex":         "elixir",
	"exs":        "elixir",
	"golang":     "go",
	"hbs":        "handlebars",
	"hs":         "haskell",
	"terraform":  "hcl",
	"tf":         "hcl",
	"jl":         "julia",
	"kt":         "kotlin",
	"kts":        "kotlin",
	"md":         "markdown",
	"objc":       "objective-c",
	"obj-c":      "objective-c",
	"proto":      "protobuf",
	"rkt":        "racket",
	"rst":        "restructuredtext",
	"rs":         "rust",
	"latex":      "tex",
	"ts":         "typescript",
	"vb.net":     "vbnet",
}

// CodeLanguage is the language the code macro is given for a fence's: lower
// case, as Confluence names every language, and an alias replaced by the name
// it stands for. Anything else is passed through, so a language mark has no
// entry for reaches Confluence as it did before.
func CodeLanguage(lang string) string {
	lang = strings.ToLower(lang)
	if name, ok := codeLanguageAliases[lang]; ok {
		return name
	}

	return lang
}
