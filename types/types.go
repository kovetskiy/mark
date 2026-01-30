package types

type MarkConfig struct {
	MermaidScale  float64
	D2Scale       float64
	D2Output      string
	D2Bundle      bool
	DropFirstH1   bool
	StripNewlines bool
	Features      []string
}
