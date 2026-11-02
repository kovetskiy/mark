package renderer

import "strings"

// codeLanguageAliases maps a fence language that some Confluence does not know
// to a name for the same language that it does. Data Center 8 and earlier fail
// to render a code macro whose language is not in their registry; Data Center
// 9 and later, like Cloud, show an unknown one unhighlighted.
//
// The two platforms resolve their own aliases, and their lists differ, so an
// entry is here only when the alias is a true synonym that at least one of
// them does not know, and the target highlights the same language wherever the
// alias already worked. TestCodeLanguageAliasTargets holds both lists and
// checks every entry against them.
var codeLanguageAliases = map[string]string{
	// Targets every platform knows.
	"as3":       "actionscript3",
	"cxx":       "cpp",
	"cc":        "cpp",
	"hpp":       "cpp",
	"cfc":       "coldfusion",
	"udiff":     "diff",
	"patch":     "diff",
	"php3":      "php",
	"php4":      "php",
	"php5":      "php",
	"posh":      "powershell",
	"ps1":       "powershell",
	"psm1":      "powershell",
	"pwsh":      "powershell",
	"py3":       "python",
	"python3":   "python",
	"rb":        "ruby",
	"sh":        "bash",
	"zsh":       "bash",
	"ksh":       "bash",
	"txt":       "text",
	"plaintext": "text",

	// Targets only Cloud and Data Center 9 know; Data Center 8 knows neither
	// the alias nor the target.
	"clj":    "clojure",
	"golang": "go",
	"hbs":    "handlebars",
	"tf":     "hcl",
	"kt":     "kotlin",
	"kts":    "kotlin",
	"md":     "markdown",
	"rs":     "rust",
}

// CodeLanguage is the language the code macro is given for a fence's: an alias
// replaced by the name it stands for, matched without regard to case. Anything
// else is passed through exactly as written.
func CodeLanguage(lang string) string {
	if name, ok := codeLanguageAliases[strings.ToLower(lang)]; ok {
		return name
	}

	return lang
}
