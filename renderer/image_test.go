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
		{"No width available", "center", "", "center"},
		{"Below threshold", "center", "500", "center"},
		{"At threshold", "center", "760", "wide"},
		{"Above threshold", "center", "1000", "wide"},
		{"Left below threshold", "left", "700", "left"},
		{"Left at threshold", "left", "760", "wide"},
		{"Invalid width", "center", "abc", "center"},
		{"Large image", "center", "2000", "wide"},
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
		{"Left alignment", "left", "500", "align-start"},
		{"Center alignment", "center", "500", "center"},
		{"Right alignment", "right", "500", "align-end"},
		{"Wide alignment", "wide", "1000", "center"},
		{"Full-width threshold", "center", "1800", "full-width"},
		{"Above full-width", "left", "2000", "full-width"},
		{"Below full-width", "center", "1799", "center"},
		{"No alignment", "", "1000", ""},
		{"Unknown alignment", "justify", "500", ""},
		{"Invalid width", "center", "abc", "center"},
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
		{"Center layout keeps original", "1000", "center", "1000"},
		{"Align-start keeps original", "800", "align-start", "800"},
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
