package types

type MarkConfig struct {
	MermaidScale float64
	D2Scale      float64
	// MathFormat is the image a formula is published as, "svg" or "png".
	// MathScale multiplies the pixels of a PNG one, and is ignored for SVG.
	MathFormat    string
	MathScale     float64
	DropFirstH1   bool
	StripNewlines bool
	Features      []string
	ImageAlign    string
	IncludePath   string

	// ResolveLink turns a link target written in the document -- a relative
	// path, optionally with a #fragment -- into the Confluence link it should
	// become, or "" to leave it as written. The text is the words between the
	// brackets, which only an ac: link with nothing after the colon uses.
	//
	// Supplied as a function so that rewriting links stays a property of the
	// document tree rather than of the bytes it was parsed from, without the
	// renderer needing to know what Confluence is. Nil disables the rewriting
	// entirely, which is what compiling without a server does.
	ResolveLink func(target, text string) (string, error)

	// ResolveAttachment turns a link or image destination into the URL of the
	// attachment uploaded for it, or "" to leave it as written. Nil disables
	// the rewriting.
	ResolveAttachment func(target string) string
}
