package main

import (
	"testing"
)

// The CFR/VFR verdict must come from the measured frame spacing: a source that drops frames
// irregularly still reports r=avg=60, so trusting the header mislabels it as CFR.
func TestClassifyCadence(t *testing.T) {
	uniform := make([]float64, 120)
	for i := range uniform {
		uniform[i] = 0.0167
	}
	if v := classifyCadence(uniform); !v.CFR || !v.OK {
		t.Fatalf("uniform 60fps should be CFR, got %+v", v)
	}

	// The shape that fooled the header check: mostly 60fps with a regular 4-frame gap.
	vfr := make([]float64, 0, 126)
	for i := 0; i < 126; i++ {
		if i%10 == 9 {
			vfr = append(vfr, 0.0667)
		} else {
			vfr = append(vfr, 0.0167)
		}
	}
	if v := classifyCadence(vfr); v.CFR || !v.OK {
		t.Fatalf("irregular spacing should be VFR, got %+v", v)
	}
	if v := classifyCadence(vfr); v.MaxGapMS < 60 {
		t.Fatalf("max gap should expose the 67ms hole, got %+v", v)
	}

	// Too few samples: not enough evidence, the caller falls back to the header.
	if v := classifyCadence([]float64{0.016, 0.017}); v.OK {
		t.Fatalf("few samples must not produce a verdict, got %+v", v)
	}

	// A 25fps source with normal encoder jitter stays CFR.
	p25 := make([]float64, 60)
	for i := range p25 {
		p25[i] = 0.040
	}
	p25[3] = 0.041
	if v := classifyCadence(p25); !v.CFR {
		t.Fatalf("25fps with 1ms jitter should be CFR, got %+v", v)
	}
}
