package syntax

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeLanguage(t *testing.T) {
	for _, tt := range []struct{ fence, want string }{
		{"sh", "bash"},
		{"SH", "bash"},
		{"zsh", "bash"},
		{"golang", "go"},
		{"txt", "text"},
		// Every platform knows these, so they are left as written.
		{"bash", "bash"},
		{"shell", "shell"},
		{"yml", "yml"},
		{"py", "py"},
		{"pl", "pl"},
		{"c++", "c++"},
		{"js", "js"},
		{"Python", "Python"},
		{"JSON", "JSON"},
		{"C#", "C#"},
		// Unknown to mark: passed through as written.
		{"zig", "zig"},
		{"Zig", "Zig"},
		{"console", "console"},
		{"", ""},
	} {
		assert.Equal(t, tt.want, CodeLanguage(tt.fence), tt.fence)
	}
}

// dataCenter8Languages is the language registry of the code macro in Confluence
// Data Center 8 and earlier (newcode-macro-plugin 18.x,
// BuiltInLanguageInstallerConstants): each language's names. The macro
// lowercases the language before looking it up, and fails to render one that
// is not here.
var dataCenter8Languages = [][]string{
	{"actionscript3", "as3", "actionscript"},
	{"applescript"},
	{"bash", "shell"},
	{"cf", "coldfusion"},
	{"c", "cpp", "c++"},
	{"c#", "c-sharp", "csharp"},
	{"css"},
	{"delphi", "pascal"},
	{"diff", "patch"},
	{"erl", "erlang"},
	{"groovy"},
	{"java"},
	{"javafx"},
	{"js", "jscript", "javascript"},
	{"perl", "pl"},
	{"php"},
	{"plain", "text", "none"},
	{"powershell", "ps"},
	{"py", "python"},
	{"ruby", "rails", "ror"},
	{"sass"},
	{"scala"},
	{"sql"},
	{"vb", "vb.net", "vbnet"},
	{"xml", "html", "xhtml"},
	{"yaml", "yml"},
}

// cloudLanguages is SUPPORTED_LANGUAGES of @atlaskit/code, which Cloud renders
// code blocks with and the code macro of Data Center 9 and later
// (newcode-macro-plugin 19.x and up) bundles: each language's name and
// aliases. A language is matched case-sensitively against the name or an
// alias, and one that matches nothing is shown as plain text.
var cloudLanguages = []struct{ name, aliases string }{
	{"PHP", "php php3 php4 php5"},
	{"Java", "java"},
	{"CSharp", "csharp c#"},
	{"Python", "python py"},
	{"JavaScript", "javascript js"},
	{"Html", "html"},
	{"C++", "c++ cpp clike"},
	{"Ruby", "ruby rb duby"},
	{"Objective-C", "objective-c objectivec obj-c objc"},
	{"C", "c"},
	{"Swift", "swift"},
	{"TeX", "tex latex"},
	{"Shell", "shell bash sh ksh zsh"},
	{"Scala", "scala"},
	{"Go", "go"},
	{"ActionScript", "actionscript actionscript3 as"},
	{"ColdFusion", "coldfusion"},
	{"JavaFX", "javafx jfx"},
	{"VbNet", "vbnet vb.net vfp clipper xbase"},
	{"JSON", "json"},
	{"MATLAB", "matlab"},
	{"Groovy", "groovy"},
	{"SQL", "sql postgresql postgres plpgsql psql postgresql-console postgres-console tsql t-sql mysql sqlite"},
	{"R", "r"},
	{"Perl", "perl pl"},
	{"Lua", "lua"},
	{"Pascal", "pas pascal objectpascal delphi"},
	{"XML", "xml"},
	{"TypeScript", "typescript ts"},
	{"CoffeeScript", "coffeescript coffee-script coffee"},
	{"Haskell", "haskell hs"},
	{"Puppet", "puppet"},
	{"Arduino", "arduino"},
	{"Fortran", "fortran"},
	{"Erlang", "erlang erl"},
	{"PowerShell", "powershell posh ps1 psm1"},
	{"Haxe", "haxe hx hxsl"},
	{"Elixir", "elixir ex exs"},
	{"Verilog", "verilog v"},
	{"Rust", "rust"},
	{"VHDL", "vhdl"},
	{"Sass", "sass"},
	{"OCaml", "ocaml"},
	{"Dart", "dart"},
	{"CSS", "css"},
	{"reStructuredText", "restructuredtext rst rest"},
	{"Kotlin", "kotlin"},
	{"D", "d"},
	{"Octave", "octave"},
	{"QML", "qbs qml"},
	{"Prolog", "prolog"},
	{"FoxPro", "foxpro purebasic"},
	{"Scheme", "scheme scm"},
	{"CUDA", "cuda cu"},
	{"Julia", "julia jl"},
	{"Racket", "racket rkt"},
	{"Ada", "ada ada95 ada2005"},
	{"Tcl", "tcl"},
	{"Mathematica", "mathematica mma nb"},
	{"Autoit", "autoit"},
	{"StandardML", "standardmL sml standardml"},
	{"Objective-J", "objective-j objectivej obj-j objj"},
	{"Smalltalk", "smalltalk squeak st"},
	{"Vala", "vala vapi"},
	{"LiveScript", "livescript live-script"},
	{"XQuery", "xquery xqy xq xql xqm"},
	{"PlainText", "text plaintext"},
	{"Yaml", "yaml yml"},
	{"GraphQL", "graphql"},
	{"AppleScript", "applescript"},
	{"Clojure", "clojure"},
	{"Diff", "diff"},
	{"VisualBasic", "visualbasic"},
	{"JSX", "jsx"},
	{"TSX", "tsx"},
	{"SplunkSPL", "splunk-spl"},
	{"Dockerfile", "docker dockerfile"},
	{"HCL", "hcl terraform"},
	{"NGINX", "nginx"},
	{"ProtocolBuffers", "protobuf proto"},
	{"TOML", "toml"},
	{"Handlebars", "handlebars mustache"},
	{"Gherkin", "gherkin cucumber"},
	{"ABAP", "abap"},
	{"Markdown", "markdown"},
}

func languageIndex(languages [][]string) map[string]int {
	index := map[string]int{}
	for i, names := range languages {
		for _, name := range names {
			index[name] = i
		}
	}

	return index
}

// TestCodeLanguageAliasTargets checks every alias against both platforms'
// lists: the rename must never cost a platform a language it highlighted, and
// must gain one somewhere.
func TestCodeLanguageAliasTargets(t *testing.T) {
	dataCenter8 := languageIndex(dataCenter8Languages)

	cloudNames := make([][]string, len(cloudLanguages))
	for i, language := range cloudLanguages {
		cloudNames[i] = append([]string{language.name}, strings.Fields(language.aliases)...)
	}
	cloud := languageIndex(cloudNames)

	aliases := codeLanguageAliases
	require.NotEmpty(t, aliases)

	for alias, target := range aliases {
		assert.Equal(t, strings.ToLower(alias), alias, "an alias is matched in lower case")
		assert.Equal(t, target, CodeLanguage(target), "%s: the target %s must map to itself", alias, target)

		aliasCloudLanguage, aliasCloud := cloud[alias]
		targetCloudLanguage, targetCloud := cloud[target]
		if assert.True(t, targetCloud, "%s: Cloud does not know the target %s", alias, target) && aliasCloud {
			assert.Equal(t, aliasCloudLanguage, targetCloudLanguage, "%s: the target %s is another language on Cloud", alias, target)
		}

		aliasDCLanguage, aliasDC := dataCenter8[alias]
		targetDCLanguage, targetDC := dataCenter8[target]
		if aliasDC {
			assert.True(t, targetDC, "%s: Data Center 8 knows the alias but not the target %s", alias, target)
			assert.Equal(t, aliasDCLanguage, targetDCLanguage, "%s: the target %s is another language on Data Center 8", alias, target)
		}

		assert.True(t, (!aliasDC && targetDC) || (!aliasCloud && targetCloud),
			"%s: no platform knows %s but not %s, so renaming it gains nothing", alias, target, alias)
	}
}
