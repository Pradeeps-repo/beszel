package system

import "testing"

func TestNormalizeDisplay(t *testing.T) {
	if got := NormalizeDisplay("1080x1920"); got != "1080x1920" {
		t.Fatalf("single value: %q", got)
	}
	repeated := ""
	for i := 0; i < 200; i++ {
		repeated += "1080x1920"
	}
	if got := NormalizeDisplay(repeated); got != "1080x1920" {
		t.Fatalf("repeated value: %q", got)
	}
	if got := NormalizeDisplay("1080x1920, 1920x1080, 1080x1920"); got != "1080x1920, 1920x1080" {
		t.Fatalf("distinct values: %q", got)
	}
	if got := NormalizeLabel("portrait (90°)"); got != "portrait (90°)" {
		t.Fatalf("orientation: %q", got)
	}
	portrait := ""
	for i := 0; i < 50; i++ {
		portrait += "portrait"
	}
	if got := NormalizeLabel(portrait); got != "portrait" {
		t.Fatalf("repeated orientation: %q", got)
	}
}
