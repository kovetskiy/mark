package types

type MarkConfig struct {
	MermaidProvider string
	MermaidScale    float64
	D2Scale         float64
	DropFirstH1     bool
	StripNewlines   bool
	Features        []string
}
