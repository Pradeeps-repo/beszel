package agentupdate

import (
	"testing"
	"time"
)

func TestCompareFleetStamps(t *testing.T) {
	if Compare("0.21.2", "0.21.2-bw.3+abc") >= 0 {
		t.Fatal("plain 0.21.2 is older than a stamped build")
	}
	if Compare("0.21.2-bw.3+aaa", "0.21.2-bw.12+bbb") >= 0 {
		t.Fatal("run 3 is older than run 12")
	}
	if Compare("0.21.2-bw.4+aaa", "0.21.2-bw.4+bbb") != 0 {
		t.Fatal("commit metadata does not change order")
	}
	if Compare("0.21.3", "0.21.2-bw.99") <= 0 {
		t.Fatal("a newer core version wins")
	}
}

func TestWindowAndRollout(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	night := time.Date(2026, 11, 24, 2, 0, 0, 0, loc)
	day := time.Date(2026, 11, 24, 12, 0, 0, 0, loc)
	if !InWindow(night, "01:00", "05:00", loc) || InWindow(day, "01:00", "05:00", loc) {
		t.Fatal("overnight window")
	}
	if !InWindow(time.Date(2026, 11, 24, 23, 0, 0, 0, loc), "22:00", "06:00", loc) {
		t.Fatal("window should cross midnight")
	}
	if !InRollout("same", "kiosk", 100, nil) || InRollout("same", "kiosk", 0, nil) {
		t.Fatal("percent bounds")
	}
	if !InRollout("id-1", "kiosk-a", 0, []string{"kiosk-a"}) {
		t.Fatal("canary name should match")
	}
	if InRollout("id-2", "other", 100, []string{"kiosk-a"}) {
		t.Fatal("canary list replaces the percentage")
	}
	first := InRollout("alpha", "alpha", 10, nil)
	if InRollout("alpha", "alpha", 10, nil) != first {
		t.Fatal("percentage must be stable")
	}
}

func TestAutoEligibleAndBatch(t *testing.T) {
	settings := Defaults()
	settings.Enabled = true
	settings.Percent = 100
	now := time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC)
	behind := System{ID: "1", Name: "a", Status: "up", Version: "0.21.2"}
	if !AutoEligible(behind, settings, "0.21.2-bw.5+abc", now, false) {
		t.Fatal("enabled rollout should include a system that is behind")
	}
	settings.Enabled = false
	if AutoEligible(behind, settings, "0.21.2-bw.5+abc", now, false) {
		t.Fatal("auto update is off")
	}
	if !AutoEligible(behind, settings, "0.21.2-bw.5+abc", time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC), true) {
		t.Fatal("force bypasses the switch and the window")
	}
	day := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	settings.Enabled = true
	if AutoEligible(behind, settings, "0.21.2-bw.5+abc", day, false) {
		t.Fatal("outside the window")
	}
	batch := Batch([]System{{ID: "b", Name: "b"}, {ID: "a", Name: "a", Forced: true}, {ID: "c", Name: "c"}}, 2)
	if len(batch) != 2 || !batch[0].Forced || batch[1].Name != "b" {
		t.Fatalf("batch = %+v", batch)
	}
}

func TestReleaseURL(t *testing.T) {
	base := ReleaseBase(DefaultRepo, DefaultTag)
	if err := ValidateReleaseBase(base, DefaultRepo); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReleaseBase("https://evil.example/beszel", DefaultRepo); err == nil {
		t.Fatal("other hosts must be rejected")
	}
	if err := ValidateReleaseBase("https://github.com/other/beszel/releases/download/agent-latest", DefaultRepo); err == nil {
		t.Fatal("other repos must be rejected")
	}
	if err := ValidateReleaseBase(base+"?file=x", DefaultRepo); err == nil {
		t.Fatal("query strings must be rejected")
	}
	file, err := ParseVersionFile("version=0.21.2-bw.9+abc\ncommit=abc\n")
	if err != nil || file.Version != "0.21.2-bw.9+abc" || file.Commit != "abc" {
		t.Fatalf("version file: %+v %v", file, err)
	}
	sum, err := ParseSHA256("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  beszel-agent\n")
	if err != nil || len(sum) != 64 {
		t.Fatal(sum, err)
	}
}
