package renderer

import "testing"

func TestInvalidD2OutputError(t *testing.T) {
	err := invalidD2OutputError("pdf")
	if err == nil {
		t.Fatalf("expected error")
	}

	want := `unsupported d2-output "pdf": must be one of png or svg`
	if err.Error() != want {
		t.Fatalf("expected %q, got %q", want, err.Error())
	}
}
