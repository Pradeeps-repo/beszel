package records

import (
	"testing"

	"github.com/henrygd/beszel/internal/entities/system"
)

func uint8Ptr(v uint8) *uint8 { return &v }

func TestAverageSystemStatsBrightness(t *testing.T) {
	result := AverageSystemStatsSlice([]system.Stats{
		{Brightness: uint8Ptr(0)},
		{},
		{Brightness: uint8Ptr(50)},
	})
	if result.Brightness == nil || *result.Brightness != 25 {
		t.Fatalf("brightness = %v, want 25", result.Brightness)
	}

	zero := AverageSystemStatsSlice([]system.Stats{{Brightness: uint8Ptr(0)}, {Brightness: uint8Ptr(0)}})
	if zero.Brightness == nil || *zero.Brightness != 0 {
		t.Fatalf("zero brightness = %v, want 0", zero.Brightness)
	}

	absent := AverageSystemStatsSlice([]system.Stats{{}, {Cpu: 10}})
	if absent.Brightness != nil {
		t.Fatalf("absent brightness = %v, want nil", absent.Brightness)
	}
	if absent.Cpu != 5 {
		t.Fatalf("cpu = %v, want 5", absent.Cpu)
	}
}
