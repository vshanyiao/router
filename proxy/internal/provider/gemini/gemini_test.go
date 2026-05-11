package gemini

import "testing"

func TestNew(t *testing.T) {
	a := New()
	if a.HTTP == nil {
		t.Fatal("expected HTTP client to be set")
	}
}
