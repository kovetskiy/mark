package syntax

import (
	"regexp"
	"strings"
)

var reBlockDetails = regexp.MustCompile(
	// (<Lang>|-) (collapse|<theme>|\d)* (title <title>)?
	//
	// The language class is [\w#+./-] rather than \w because real language
	// names carry non-word characters: c#, c++, objective-c, html/xml, .NET.
	`^(?:([\w#+./-]*)|-)\s*(\S.*?\S?)??\s*(?:\btitle\s+(\S.*\S?))?$`,
)

// reInfoAttributes matches the "{...}" attribute list Pandoc and Material for
// MkDocs write in an info string -- "``` { .js hl_lines="1 2" }" or
// "```python {.line-numbers}".
//
// None of what it holds is mark's option vocabulary, so it is taken out before
// the options are read. Anchored to the ends of the info string, which is the
// only place Pandoc and MkDocs put one, so a brace inside a title stays.
var reInfoAttributes = regexp.MustCompile(`^\{[^}]*\}|\{[^}]*\}$`)

// reAttributeLang matches the ".language" class inside an attribute block,
// which is where the language is named when the block is the whole info
// string.
var reAttributeLang = regexp.MustCompile(`\.([\w#+./-]+)`)

// reAssignedTitle matches a title written with "=" rather than a space.
//
// Material for MkDocs' title="config.yml" is the commonest way to caption a
// code block in a docs-as-code repository.
var reAssignedTitle = regexp.MustCompile(`\btitle\s*=\s*(?:"([^"]*)"|'([^']*)'|(\S+))`)

// ParseCodeBlockInfo reads a fence info string into the language, options
// and title the ac:code macro is given.
func ParseCodeBlockInfo(info string) (lang string, options []string, title string) {
	info = strings.TrimSpace(info)

	// "-" is the documented way to write "no language, but there are options
	// after it" (docs/formatting.md's "```- 1 collapse midnight title ...").
	// The language class accepts hyphens, so the bare marker is checked first.
	noLanguage := false
	if info == "-" || strings.HasPrefix(info, "- ") {
		noLanguage = true
		info = strings.TrimSpace(strings.TrimPrefix(info, "-"))
	}

	if groups := reAssignedTitle.FindStringSubmatch(info); groups != nil {
		for _, group := range groups[1:] {
			if group != "" {
				title = group
				break
			}
		}
		info = strings.TrimSpace(reAssignedTitle.ReplaceAllString(info, " "))
	}

	if block := reInfoAttributes.FindString(info); block != "" {
		// Only a leading block names the language: Pandoc writes the whole
		// info string as one attribute list, while MkDocs appends the list
		// after a language it has already given in the ordinary place.
		if strings.HasPrefix(info, "{") {
			if class := reAttributeLang.FindStringSubmatch(block); class != nil {
				lang = class[1]
			}
		}
		info = strings.TrimSpace(strings.Replace(info, block, " ", 1))
	}

	// The "-" marker stands for no language and must be present for the match.
	if noLanguage {
		info = "- " + info
	}

	groups := reBlockDetails.FindStringSubmatch(info)
	if len(groups) == 0 {
		return lang, nil, title
	}

	if lang == "" && !noLanguage {
		lang = groups[1]
	}
	if title == "" {
		title = groups[3]
	}

	return lang, strings.Fields(groups[2]), title
}

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
