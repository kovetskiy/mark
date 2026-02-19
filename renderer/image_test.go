package renderer

import "testing"

func TestCalculateAlign(t *testing.T) {
	tests := []struct {
		name             string
		configuredAlign  string
		width            string
		expectedAlign    string
	}{
		{"No alignment configured", "", "1000", ""},
		{"Center alignment small", "center", "500", "center"},
		{"Left alignment small", "left", "500", "left"},
		{"Right alignment small", "right", "500", "right"},
		{"Left forced to center at 760px", "left", "760", "center"},
		{"Left forced to center above 760px", "left", "1000", "center"},
		{"Right forced to center at 1800px", "right", "1800", "center"},
		{"No width provided", "left", "", "left"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateAlign(tt.configuredAlign, tt.width)
			if result != tt.expectedAlign {
				t.Errorf("calculateAlign(%q, %q) = %q, want %q", tt.configuredAlign, tt.width, result, tt.expectedAlign)
			}
		})
	}
}

func TestCalculateLayout(t *testing.T) {
	tests := []struct {
		name           string
		align          string
		width          string
		expectedLayout string
	}{
		// Small images (< 760px) use alignment-based layout
		{"Left alignment small", "left", "500", "align-start"},
		{"Center alignment small", "center", "500", "center"},
		{"Right alignment small", "right", "500", "align-end"},
		{"No alignment small", "", "500", ""},
		
		// Medium images (760-1799px) use "wide" layout (must be center align)
		{"Center at 760px", "center", "760", "wide"},
		{"Center at 1000px", "center", "1000", "wide"},
		{"Center at 1799px", "center", "1799", "wide"},
		
		// Large images (>= 1800px) use "full-width" layout (must be center align)
		{"Center at 1800px", "center", "1800", "full-width"},
		{"Center at 2000px", "center", "2000", "full-width"},
		
		// Edge cases
		{"No width", "center", "", "center"},
		{"Invalid width", "center", "abc", "center"},
		{"Empty alignment and width", "", "", ""},
		{"No alignment configured", "", "1000", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateLayout(tt.align, tt.width)
			if result != tt.expectedLayout {
				t.Errorf("calculateLayout(%q, %q) = %q, want %q", tt.align, tt.width, result, tt.expectedLayout)
			}
		})
	}
}

func TestCalculateDisplayWidth(t *testing.T) {
	tests := []struct {
		name          string
		originalWidth string
		layout        string
		expectedWidth string
	}{
		{"Full-width layout", "2000", "full-width", "1800"},
		{"Wide layout keeps original", "1000", "wide", "1000"},
		{"Center layout keeps original", "800", "center", "800"},
		{"Align-start keeps original", "500", "align-start", "500"},
		{"Empty original", "", "center", ""},
		{"Empty layout", "1000", "", "1000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateDisplayWidth(tt.originalWidth, tt.layout)
			if result != tt.expectedWidth {
				t.Errorf("calculateDisplayWidth(%q, %q) = %q, want %q", tt.originalWidth, tt.layout, result, tt.expectedWidth)
			}
		})
	}
}
