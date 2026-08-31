package renderer

import (
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/stdlib"
	east "github.com/yuin/goldmark-emoji/ast"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// ConfluenceEmojiRenderer renders a :shortcode: as the emoji it names.
//
// Confluence has two ways of holding an emoji and understands them unevenly.
// Data Center knows only ac:emoticon's ac:name, and only the couple of dozen
// names its storage-format reference lists; Cloud writes the same tag with
// ac:emoji-id, ac:emoji-shortname and ac:emoji-fallback beside it and picks the
// glyph from those. A name Data Center does not know renders as nothing at all,
// which is why an emoji outside the legacy set is written as the character
// itself: text renders identically on both, and neither has to recognise a
// macro to show it.
type ConfluenceEmojiRenderer struct {
	Stdlib *stdlib.Lib
}

// NewConfluenceEmojiRenderer creates a new instance of the ConfluenceEmojiRenderer.
func NewConfluenceEmojiRenderer(stdlib *stdlib.Lib) renderer.NodeRenderer {
	return &ConfluenceEmojiRenderer{
		Stdlib: stdlib,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceEmojiRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(east.KindEmoji, r.renderEmoji)
}

// confluenceEmoticons maps an emoji to the legacy ac:emoticon name that stands
// for it, keyed by the id emojiID builds so that every shortcode spelling of
// one emoji -- :+1: and :thumbsup: are the same character -- is covered once.
//
// The values are the whole of what Data Center accepts, from
// https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html
// minus the names no GitHub shortcode is close enough to claim (light-off and
// the red, green and blue stars).
//
// Several emoji deliberately collapse onto one name: Data Center has one
// smiley, not the four ways Unicode has of drawing one. The distinction is not
// lost on Cloud, which reads ac:emoji-id and ignores the name.
var confluenceEmoticons = map[string]string{
	"1f600": "smile",        // grinning
	"1f603": "smile",        // smiley
	"1f604": "smile",        // smile
	"1f601": "laugh",        // grin
	"1f602": "laugh",        // joy
	"1f606": "laugh",        // laughing
	"1f609": "wink",         // wink
	"1f61b": "cheeky",       // stuck_out_tongue
	"1f61c": "cheeky",       // stuck_out_tongue_winking_eye
	"1f61e": "sad",          // disappointed
	"1f622": "sad",          // cry
	"1f626": "sad",          // frowning
	"1f44d": "thumbs-up",    // thumbsup, +1
	"1f44e": "thumbs-down",  // thumbsdown, -1
	"2139":  "information",  // information_source
	"2611":  "tick",         // ballot_box_with_check
	"2705":  "tick",         // white_check_mark
	"2714":  "tick",         // heavy_check_mark
	"274c":  "cross",        // x
	"26a0":  "warning",      // warning
	"2795":  "plus",         // heavy_plus_sign
	"2796":  "minus",        // minus
	"2753":  "question",     // question
	"2754":  "question",     // grey_question
	"1f4a1": "light-on",     // bulb
	"2b50":  "yellow-star",  // star
	"2764":  "heart",        // heart
	"1f494": "broken-heart", // broken_heart
}

// emojiID spells an emoji the way Confluence Cloud's ac:emoji-id does: the
// codepoints in lowercase hex, joined by dashes.
//
// U+FE0F is left out. It only asks for the colour rendering of a character that
// also has a monochrome form, and Cloud's ids are written without it -- :heart:
// is 2764, not 2764-fe0f. An id Cloud does not recognise is not fatal in any
// case: ac:emoji-fallback carries the character itself, which is what gets
// drawn when the id means nothing.
func emojiID(runes []rune) string {
	var id strings.Builder

	for _, r := range runes {
		if r == 0xFE0F {
			continue
		}
		if id.Len() > 0 {
			id.WriteByte('-')
		}
		id.WriteString(strconv.FormatInt(int64(r), 16))
	}

	return id.String()
}

func (r *ConfluenceEmojiRenderer) renderEmoji(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*east.Emoji)

	// definition.NewEmoji holds a shortcode Unicode has no character for as
	// U+FFFD. The bundled GitHub set has none of those today, but a definition
	// passed to goldmark-emoji's WithEmojis may; writing one would put a
	// replacement glyph on the page, so the shortcode is left as it was typed.
	if !n.Value.IsUnicode() {
		_, _ = w.WriteString(":" + string(n.ShortName) + ":")
		return ast.WalkContinue, nil
	}

	character := string(n.Value.Unicode)
	id := emojiID(n.Value.Unicode)

	name, ok := confluenceEmoticons[id]
	if !ok {
		// No legacy name to give Data Center, so the character goes on the page
		// as text. Emoji hold nothing XML-significant, so there is nothing to
		// escape.
		_, _ = w.WriteString(character)
		return ast.WalkContinue, nil
	}

	err := r.Stdlib.Templates.ExecuteTemplate(w, "ac:emoticon", struct {
		Name      string
		ShortName string
		ID        string
		Fallback  string
	}{
		Name: name,
		// The shortcode as the author spelled it, not the emoji's canonical
		// one: Cloud only shows it when the editor offers the emoji back for
		// editing, and either spelling names the same character.
		ShortName: ":" + string(n.ShortName) + ":",
		ID:        id,
		Fallback:  character,
	})
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}
