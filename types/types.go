package types

type MarkConfig struct {
	MermaidScale  float64
	MermaidOutput string
	MermaidBundle bool
	D2Scale       float64
	DropFirstH1   bool
	StripNewlines bool
	Features      []string
	ImageAlign    string
}
