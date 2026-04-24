package renderer

import "testing"

func TestParseImgAttrs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSrc     string
		wantWidth   string
		wantAlt     string
		wantTitle   string
	}{
		{
			name:      "full attributes",
			input:     `<img src="arch.png" width="600" alt="Architecture" title="Arch diagram" />`,
			wantSrc:   "arch.png",
			wantWidth: "600",
			wantAlt:   "Architecture",
			wantTitle: "Arch diagram",
		},
		{
			name:      "src and width only",
			input:     `<img src="diagram.png" width="760" />`,
			wantSrc:   "diagram.png",
			wantWidth: "760",
		},
		{
			name:    "src only",
			input:   `<img src="logo.png" />`,
			wantSrc: "logo.png",
		},
		{
			name:      "no closing slash",
			input:     `<img src="foo.png" width="400">`,
			wantSrc:   "foo.png",
			wantWidth: "400",
		},
		{
			name:  "not an img tag",
			input: `<p>hello</p>`,
		},
		{
			name:  "empty",
			input: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, width, alt, title := parseImgAttrs(tt.input)
			if src != tt.wantSrc {
				t.Errorf("src = %q, want %q", src, tt.wantSrc)
			}
			if width != tt.wantWidth {
				t.Errorf("width = %q, want %q", width, tt.wantWidth)
			}
			if alt != tt.wantAlt {
				t.Errorf("alt = %q, want %q", alt, tt.wantAlt)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
		})
	}
}
