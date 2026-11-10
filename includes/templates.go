package includes

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/internal/mdsource"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
	"go.yaml.in/yaml/v3"
)

// IncludeDirective contains parsed parameters from an <!-- Include: ... --> block.
type IncludeDirective struct {
	Template string
	Left     string
	Right    string
	Data     map[string]any
}

func findIncludeDirectiveBounds(s string) (startIdx int, endIdx int) {
	return findIncludeDirective(s, nil)
}

// findIncludeDirective finds the first Include directive that skip does not
// reject, which is how a directive shown inside a code block is passed over
// rather than acted on.
func findIncludeDirective(s string, skip func(offset int) bool) (startIdx int, endIdx int) {
	searchFrom := 0
	for {
		start := strings.Index(s[searchFrom:], "<!--")
		if start == -1 {
			return -1, -1
		}
		startIdx = searchFrom + start

		// An opener inside code opens nothing, so it is stepped over on its
		// own rather than paired with a closer. Paired, a "<!--" quoted in a
		// code span took the next "-->" in the document -- the one closing a
		// real directive further on -- and the scan resumed past it, so the
		// directive was never expanded.
		if skip != nil && skip(startIdx) {
			searchFrom = startIdx + 4
			continue
		}

		// Search past the opener: starting at startIdx lets the closer overlap it,
		// so "<!-->" matches a "-->" at offset 2 and yields a 5-byte comment whose
		// body slice comment[4:2] panics.
		end := strings.Index(s[startIdx+4:], "-->")
		if end == -1 {
			return -1, -1
		}
		endIdx = startIdx + 4 + end + 3

		comment := s[startIdx:endIdx]
		trimmed := strings.TrimSpace(comment[4 : len(comment)-3])
		if strings.HasPrefix(trimmed, "Include:") {
			return startIdx, endIdx
		}

		// A real comment is passed over whole: what it holds is not the
		// document's, and a macro written with an Include in its body is
		// expanded when the macro is used, not where it is defined.
		searchFrom = endIdx
	}
}

// ParseIncludeDirective parses an <!-- Include: ... --> HTML comment block without regex.
func ParseIncludeDirective(raw []byte) (*IncludeDirective, error) {
	s := string(raw)
	startIdx, endIdx := findIncludeDirectiveBounds(s)
	if startIdx == -1 {
		return nil, nil
	}

	comment := strings.TrimSpace(s[startIdx+4 : endIdx-3])
	if !strings.HasPrefix(comment, "Include:") {
		return nil, nil
	}

	normalizedComment := strings.ReplaceAll(comment, "\\n", "\n")
	lines := strings.Split(normalizedComment, "\n")
	firstLine := strings.TrimSpace(lines[0])
	templatePath := strings.TrimSpace(strings.TrimPrefix(firstLine, "Include:"))
	if templatePath == "" {
		return nil, nil
	}

	dir := &IncludeDirective{
		Template: templatePath,
		Data:     make(map[string]any),
	}

	var configLines []string
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Delims:") {
			delimVal := strings.TrimSpace(strings.TrimPrefix(trimmed, "Delims:"))
			if delimVal == "none" {
				dir.Left = "\x00"
				dir.Right = "\x01"
			} else {
				parts := strings.Split(delimVal, ",")
				if len(parts) == 2 {
					dir.Left = strings.Trim(strings.TrimSpace(parts[0]), `"`)
					dir.Right = strings.Trim(strings.TrimSpace(parts[1]), `"`)
				}
			}
		} else if trimmed != "" {
			configLines = append(configLines, line)
		}
	}

	if len(configLines) > 0 {
		configStr := strings.Join(configLines, "\n")
		err := yaml.Unmarshal([]byte(configStr), &dir.Data)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal template data config (path=%q, config=%q): %w", templatePath, configStr, err)
		}
	}

	return dir, nil
}

// place is a file or directory an include is read from: on the local disk,
// where name is an OS path, or in a document's fs.FS, where it is the path
// inside it. --include-path is always on disk, so one document's includes may
// come from both.
type place struct {
	root attachment.Root
	name string
}

func disk(name string) place { return place{name: name} }

func (p place) inFS() bool { return p.root.FS() != nil }

// join is the place rel names from p.
func (p place) join(rel string) place {
	if p.inFS() {
		return place{root: p.root, name: path.Join(filepath.ToSlash(p.name), filepath.ToSlash(rel))}
	}

	return disk(filepath.Join(p.name, rel))
}

func (p place) dir() place {
	if p.inFS() {
		return place{root: p.root, name: path.Dir(p.name)}
	}

	return disk(filepath.Dir(p.name))
}

// key makes p the name of exactly one file, as absolute does on disk; a path
// inside an fs.FS is one already.
func (p place) key() place {
	if p.inFS() {
		return p
	}

	return disk(absolute(p.name))
}

// cacheKey is what p is known by among the templates and the include stack.
// One in an fs.FS is marked, so that it cannot be taken for a path on disk.
func (p place) cacheKey() string {
	if p.inFS() {
		return "fs:" + p.name
	}

	return p.name
}

func (p place) exists() bool {
	_, err := p.root.Stat(p.name)

	return err == nil
}

func (p place) String() string { return p.name }

// resolveTemplatePath names the file a directive refers to.
//
// The document's own directory first, then --include-path, which is the order
// the read used to try them in. A name that matches nothing resolves against
// the document's directory anyway, so the failure to read it names the place
// the author would look first.
func resolveTemplatePath(base place, includePath, path string) place {
	candidate := base.join(path)
	if candidate.exists() {
		return candidate.key()
	}

	if includePath != "" {
		fallback := disk(filepath.Join(includePath, path))
		if fallback.exists() {
			return fallback.key()
		}
	}

	return candidate.key()
}

// resolveNestedTemplatePath names the file a directive inside an included
// fragment refers to.
//
// A fragment reads as a document in its own right, so a name it writes is
// looked for beside the fragment first -- the way the links it holds already
// are. Nested includes used to be resolved against the root document's
// directory only, and a fragment that works that way is not something anyone
// should have to rewrite, so that is still tried when nothing is beside the
// fragment. A name that matches nothing resolves beside the fragment, so the
// failure to read it names the place the author would look first.
func resolveNestedTemplatePath(base, from place, includePath, path string, warnings *report.Warnings) place {
	candidate := from.join(path).key()
	rootRelative := resolveTemplatePath(base, includePath, path)

	rootFound := rootRelative.exists()

	if candidate.exists() {
		// Only worth saying when the old rule would have picked a different
		// file: that is the one case where what a page publishes has changed.
		if rootFound && rootRelative.cacheKey() != candidate.cacheKey() {
			warnings.Add(report.WarningInclude,
				"include %q resolves beside the fragment that names it, to %s rather than %s; "+
					"write the path from the fragment's directory to include the other one",
				path, candidate, rootRelative,
			)
		}

		return candidate
	}

	if rootFound {
		log.Debug().Msgf(
			"include %q is not beside the fragment in %s, using %s found relative to the document",
			path, from, rootRelative,
		)

		return rootRelative
	}

	return candidate
}

// allowedTemplatePath reports whether a document may include the file it named.
//
// The document's own directory, or the one mark is running in -- which for a
// run at the root of a repository is the repository -- and --include-path
// besides. That directory is a permitted root because the operator chose it and
// the document did not: it is the one place a document is invited to reach that
// is not its own. A document read from an fs.FS may include what is inside it,
// and from disk only what --include-path allows.
func allowedTemplatePath(base place, includePath string, resolved place) error {
	if resolved.inFS() {
		if resolved.root.CheckReadable(".", resolved.name) != nil {
			return fmt.Errorf(
				"%w: %s is outside the document's file system, so it is not this document's to include",
				attachment.ErrOutsideProject, resolved,
			)
		}

		return nil
	}

	if !base.inFS() && attachment.CheckReadable(base.name, resolved.name) == nil {
		return nil
	}

	if includePath != "" && attachment.CheckReadable(includePath, resolved.name) == nil {
		return nil
	}

	// Reported in the words of what was asked for. The check is the one
	// attachments use and says so in its own message, which names an attachment
	// nobody wrote: what this document wrote was an include.
	if base.inFS() {
		return fmt.Errorf(
			"%w: %s is on disk outside --include-path %q, so it is not this document's to include",
			attachment.ErrOutsideProject, resolved, includePath,
		)
	}

	where := fmt.Sprintf("%q", base.name)
	if includePath != "" {
		where = fmt.Sprintf("%q, %q", base.name, includePath)
	}

	return fmt.Errorf(
		"%w: %s is outside %s and the directory mark is running in, so it is not this document's to include",
		attachment.ErrOutsideProject, resolved, where,
	)
}

// absolute makes a path the key of exactly one file, whatever directory the
// run was started from. A path that cannot be made absolute is returned as it
// was: it is still a better key than the bare name, and the read that follows
// will report whatever is wrong with it.
func absolute(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}

	return path
}

func LoadTemplate(
	base string,
	includePath string,
	path string,
	left string,
	right string,
	templates *template.Template,
) (*template.Template, error) {
	return LoadTemplateIn(attachment.Root{}, base, includePath, path, left, right, templates)
}

// LoadTemplateIn is LoadTemplate for a document whose directory, base, is in
// root: a fragment beside it is read from there, and one under includePath
// from disk.
func LoadTemplateIn(
	root attachment.Root,
	base string,
	includePath string,
	path string,
	left string,
	right string,
	templates *template.Template,
) (*template.Template, error) {
	// Which file this is, decided before the cache is consulted rather than
	// after. One template set is shared by every document in a run, so keying
	// on the name a directive wrote made "partial.md" beside one document the
	// same entry as "partial.md" beside another: the first one read was then
	// served to every document that asked for that name, publishing one page's
	// fragment onto another's, silently and with no error anywhere.
	from := place{root: root, name: base}
	resolved := resolveTemplatePath(from, includePath, path)

	loaded, _, err := loadTemplate(from, includePath, resolved, path, left, right, templates)

	return loaded, err
}

// loadTemplate loads what a directive named, already resolved to a file, and
// reports whether it was that file rather than a stdlib template of the name.
func loadTemplate(
	base place,
	includePath string,
	resolved place,
	path string,
	left string,
	right string,
	templates *template.Template,
) (*template.Template, bool, error) {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	name := strings.TrimSuffix(cleanPath, filepath.Ext(cleanPath))

	// A stdlib template (ac:box, ac:status, ...) is pre-registered under its plain
	// name and has no delimiters of its own, so it must always be found by that
	// name.
	if template := templates.Lookup(name); template != nil {
		return template, false, nil
	}

	// The delimiters are part of what makes the parse, so they belong in the
	// key too. Without them a file included twice with different Delims: reused
	// the first parse and silently discarded the second set.
	cacheName := resolved.cacheKey()
	if left != "" || right != "" {
		cacheName = cacheName + "\x00" + left + "\x00" + right
	}

	if template := templates.Lookup(cacheName); template != nil {
		return template, true, nil
	}

	// Held to the boundary an attachment is held to, and for the same reason.
	// A directive names a file and the file becomes page content, so
	// "../../../../etc/hostname" publishes it -- while the same path written as
	// an image is refused, which is the asymmetry rather than the decision.
	if err := allowedTemplatePath(base, includePath, resolved); err != nil {
		return nil, false, err
	}

	body, err := resolved.root.ReadFile(resolved.name)
	if err != nil {
		return nil, false, fmt.Errorf("unable to read template file %q: %w", path, err)
	}

	body = bytes.ReplaceAll(
		body,
		[]byte("\r\n"),
		[]byte("\n"),
	)

	// An included file may mark regions as not for Confluence just as the
	// document that includes it can. Stripping happens per file as each is
	// read, rather than once over the finished document, because by the time
	// the pieces are assembled the markers have already been through template
	// expansion -- and because the whole point of the feature is that the file
	// reads well on its own, which is as true of a fragment as of a page.
	body, err = mdsource.StripIgnoredBlocks(body)
	if err != nil {
		return nil, false, fmt.Errorf("unable to process template file %q: %w", path, err)
	}

	body = protectMacroDirectives(body, left, right)

	templates, err = templates.New(cacheName).Delims(left, right).Parse(string(body))
	if err != nil {
		return nil, false, fmt.Errorf("unable to parse template %q: %w", name, err)
	}

	return templates, true, nil
}

// protectMacroDirectives keeps a file's template pass out of the <!-- Macro: -->
// directives it holds.
//
// A fragment is a template, and it is executed with the include's own data
// before the document is read for macros. A macro body is a template too, but
// one for macro.ExtractMacros to run later with the macro's own arguments, so
// "{{ .Name }}" in it was answered here, by data the include never had, and
// published as <no value>. Each opening delimiter inside a directive becomes an
// action that prints it, which leaves the directive as written.
//
// The directive's extent is found by MacroDirectiveEnd, as macro.ExtractMacros
// finds it. A directive shown inside code is only an
// example, which ExtractMacros leaves alone, so it is expanded as before.
func protectMacroDirectives(body []byte, left, right string) []byte {
	if left == "\x00" {
		// "Delims: none": nothing is an action.
		return body
	}
	if left == "" {
		left = "{{"
	}
	if right == "" {
		right = "}}"
	}

	s := string(body)
	if !strings.Contains(s, left) {
		return body
	}

	code := mdsource.CodeRegions(body)

	var out strings.Builder
	last, from := 0, 0

	for {
		start := NextMacroDirective(s, from)
		if start == -1 {
			break
		}

		end := start + MacroDirectiveEnd(s[start:])
		from = end

		if mdsource.InCode(code, start) {
			continue
		}

		out.WriteString(s[last:start])
		out.WriteString(escapeDelims(s[start:end], left, right))
		last = end
	}

	out.WriteString(s[last:])

	return []byte(out.String())
}

// escapeDelims turns each occurrence of left in s into an action printing it.
func escapeDelims(s, left, right string) string {
	return strings.ReplaceAll(s, left, left+`"`+left+`"`+right)
}

// maxDirectivesPerLevel bounds how many directives a single level of a document
// may expand. The stack below names every cycle it can see, but a template that
// grows the document by some route the stack cannot express would otherwise be
// expanded until the process ran out of memory; failing loudly is better.
const maxDirectivesPerLevel = 1000

func ProcessIncludes(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
) (*template.Template, []byte, bool, error) {
	return ProcessIncludesWithStack(base, includePath, contents, templates, nil)
}

// ProcessIncludesWithStack expands the directives in contents as if they were
// found inside the chain of includes named by stack, each written relative to
// base as a directive in the document would be.
func ProcessIncludesWithStack(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
	stack []string,
) (*template.Template, []byte, bool, error) {
	return ProcessIncludesWithWarnings(base, includePath, contents, templates, stack, nil)
}

// ProcessIncludesWithWarnings is ProcessIncludesWithStack, adding to warnings
// what is worth saying about the directives it expands; nil only logs it.
func ProcessIncludesWithWarnings(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
	stack []string,
	warnings *report.Warnings,
) (*template.Template, []byte, bool, error) {
	return ProcessIncludesIn(attachment.Root{}, base, includePath, contents, templates, stack, warnings)
}

// ProcessIncludesIn is ProcessIncludesWithWarnings for a document whose
// directory, base, is in root: what it includes is read from there, held to
// root's boundary, or from includePath on disk.
func ProcessIncludesIn(
	root attachment.Root,
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
	stack []string,
	warnings *report.Warnings,
) (*template.Template, []byte, bool, error) {
	from := place{root: root, name: base}

	frames := make([]includeFrame, 0, len(stack))
	for _, name := range stack {
		frames = append(frames, includeFrame{
			name: name,
			file: resolveTemplatePath(from, includePath, name).cacheKey(),
		})
	}

	return processIncludes(from, from, includePath, contents, templates, frames, warnings)
}

// includeFrame is one include being expanded: the name the directive wrote,
// which is what an error reports, and the file it resolved to, which is what a
// cycle is detected by. Two fragments may each include a "b.md" of their own,
// and one fragment may reach the same file by two different spellings.
type includeFrame struct {
	name string
	file string
}

// processIncludes expands every directive in contents, which was read from a
// file in from. The document's own directory, base, is what the containment
// check is held to however deep the nesting, so a fragment cannot widen what a
// document may read by including something from further out.
func processIncludes(
	base place,
	from place,
	includePath string,
	contents []byte,
	templates *template.Template,
	stack []includeFrame,
	warnings *report.Warnings,
) (*template.Template, []byte, bool, error) {
	names := func() string {
		parts := make([]string, 0, len(stack))
		for _, frame := range stack {
			parts = append(parts, frame.name)
		}
		return strings.Join(parts, " -> ")
	}

	formatVardump := func(data map[string]any) string {
		var parts []string
		for key, value := range data {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
		return strings.Join(parts, ", ")
	}

	// Every directive at this level is drained here, under one stack, rather
	// than one per call with the caller looping. A sibling directive that
	// re-included an ancestor was never compared against the stack -- the
	// caller's loop started each pass with an empty one -- so
	// "a.md" holding an include of "b.md" next to an include of itself grew the
	// document without bound instead of being reported as circular.
	modified := false
	for expansions := 0; ; expansions++ {
		if expansions >= maxDirectivesPerLevel {
			return templates, contents, false, fmt.Errorf(
				"more than %d include directives expanded at one level (stack: %s), giving up",
				maxDirectivesPerLevel,
				names(),
			)
		}

		s := string(contents)

		// A directive inside a code block is a code sample: a page documenting how
		// to write an Include had the file pulled in where its own example should
		// have been.
		code := mdsource.CodeRegions(contents)
		startIdx, endIdx := findIncludeDirective(s, func(offset int) bool {
			return mdsource.InCode(code, offset)
		})
		if startIdx == -1 {
			return templates, contents, modified, nil
		}

		rawDirective := contents[startIdx:endIdx]
		dir, err := ParseIncludeDirective(rawDirective)
		if err != nil {
			return templates, contents, false, err
		}
		if dir == nil {
			return templates, contents, modified, nil
		}

		// A directive in the document names a file from the document's
		// directory; one inside an included fragment names it from the
		// fragment's, with the document's still tried after it.
		resolved := resolveTemplatePath(base, includePath, dir.Template)
		if from.cacheKey() != base.cacheKey() {
			resolved = resolveNestedTemplatePath(base, from, includePath, dir.Template, warnings)
		}

		// Detect circular include loops
		for _, frame := range stack {
			if frame.file == resolved.cacheKey() {
				return templates, contents, false, fmt.Errorf("circular include detected: %s -> %s", names(), dir.Template)
			}
		}

		log.Trace().Interface("vardump", dir.Data).Msgf("including template %q", dir.Template)

		// loadTemplate returns the specific template it loaded, whose name encodes the
		// delimiters. Execute it directly rather than looking it up by path again,
		// which would find the wrong parse when the same file is included twice with
		// different Delims:.
		loaded, isFile, err := loadTemplate(base, includePath, resolved, dir.Template, dir.Left, dir.Right, templates)
		if err != nil {
			return templates, contents, false, fmt.Errorf("unable to load template %q: %w", dir.Template, err)
		}
		templates = loaded

		// What the fragment includes in turn is written from where it lives. A
		// stdlib template lives nowhere, so its output is read from here.
		nested := from
		if isFile {
			nested = resolved.dir()
		}

		var buffer bytes.Buffer
		err = loaded.Execute(&buffer, dir.Data)
		if err != nil {
			return templates, contents, false, fmt.Errorf("unable to execute template %q (vars: %s): %w", dir.Template, formatVardump(dir.Data), err)
		}

		// A parameter holding an element must hold nothing else, and a template is
		// written to be read rather than to be careful about that.
		expanded := TrimElementParameters(buffer.Bytes())

		// Recursively process nested includes with updated stack. The stack is
		// copied rather than appended in place: every branch off this level
		// would otherwise share -- and overwrite -- the same backing array.
		newStack := make([]includeFrame, 0, len(stack)+1)
		newStack = append(newStack, stack...)
		newStack = append(newStack, includeFrame{name: dir.Template, file: resolved.cacheKey()})

		subTemplates, subBytes, _, subErr := processIncludes(base, nested, includePath, expanded, templates, newStack, warnings)
		if subErr != nil {
			return templates, contents, false, subErr
		}
		templates = subTemplates

		var res bytes.Buffer
		res.Write(contents[:startIdx])
		res.Write(subBytes)
		res.Write(contents[endIdx:])

		contents = res.Bytes()
		modified = true
	}
}

// DirectiveTargets reports the files a document asks to include, in the order
// they appear and skipping any shown inside a code block.
//
// Wanted so that a relative link written in an included fragment can be
// resolved against the fragment's own directory: the fragment reads as a
// document in its own right, and its links are written from where it lives
// rather than from wherever it is pulled into.
func DirectiveTargets(contents []byte) []string {
	s := string(contents)
	code := mdsource.CodeRegions(contents)

	var targets []string
	searchFrom := 0

	for searchFrom < len(s) {
		start, end := findIncludeDirective(s[searchFrom:], func(offset int) bool {
			return mdsource.InCode(code, searchFrom+offset)
		})
		if start == -1 {
			break
		}

		directive, err := ParseIncludeDirective([]byte(s[searchFrom+start : searchFrom+end]))
		if err == nil && directive != nil && directive.Template != "" {
			targets = append(targets, directive.Template)
		}

		searchFrom += end
	}

	return targets
}
