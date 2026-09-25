package crap

import (
	"math"
	"testing"
)

func TestScore(t *testing.T) {
	tests := []struct {
		name       string
		complexity int
		cov        float64
		want       float64
	}{
		{"primitive uncovered", 1, 0, 2},
		{"primitive covered", 1, 1, 1},
		{"cc5 uncovered", 5, 0, 30},
		{"cc5 half", 5, 0.5, 5*5*math.Pow(0.5, 3) + 5},
		{"cc12 45pct", 12, 0.45, 12*12*math.Pow(0.55, 3) + 12},
		{"fully covered equals cc", 20, 1, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Score(tt.complexity, tt.cov)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("Score(%d, %v) = %v, want %v", tt.complexity, tt.cov, got, tt.want)
			}
		})
	}
}

func TestOverThresholdStrict(t *testing.T) {
	// CRAP == threshold must pass (strict >), matching crap4j/crap4ts.
	// CC 5, cov 0 -> CRAP 30; threshold 30 must not trip.
	f := Function{Complexity: 5, Covered: 0, Total: 10}
	if OverThreshold(f, 30) {
		t.Fatal("score == threshold must pass")
	}
	f2 := Function{Complexity: 6, Covered: 0, Total: 10} // CRAP 42 > 30
	if !OverThreshold(f2, 30) {
		t.Fatal("score > threshold must fail")
	}
	// Unmeasured never trips.
	un := Function{Complexity: 99, Covered: 0, Total: 0}
	if OverThreshold(un, 30) {
		t.Fatal("unmeasured must never be over threshold")
	}
}

func TestEvaluate(t *testing.T) {
	fns := []Function{
		{Language: LanguagePython, File: "a.py", Name: "bad", Complexity: 6, Covered: 0, Total: 10},   // 42 > 30
		{Language: LanguagePython, File: "a.py", Name: "ok", Complexity: 5, Covered: 0, Total: 10},    // 30 == 30 pass
		{Language: LanguageJava, File: "b.java", Name: "tight", Complexity: 8, Covered: 0, Total: 10}, // 72 > 8
		{Language: LanguageJava, File: "b.java", Name: "unmeasured", Complexity: 3, Covered: 0, Total: 0},
	}
	thresholds := map[string]float64{LanguagePython: 30, LanguageJava: 8}
	res := Evaluate(fns, thresholds, 30)

	if res.Evaluated != 3 {
		t.Fatalf("Evaluated = %d, want 3", res.Evaluated)
	}
	if res.Unmeasured != 1 {
		t.Fatalf("Unmeasured = %d, want 1", res.Unmeasured)
	}
	if len(res.Over) != 2 {
		t.Fatalf("len(Over) = %d, want 2", len(res.Over))
	}
	if res.Over[0].Function.Name != "tight" { // 72 highest
		t.Fatalf("worst over = %s, want tight", res.Over[0].Function.Name)
	}
	if res.Worst != 72 {
		t.Fatalf("Worst = %v, want 72", res.Worst)
	}
	if res.Thresholds[LanguagePython] != 30 || res.Thresholds[LanguageJava] != 8 {
		t.Fatalf("Thresholds = %v", res.Thresholds)
	}
}

func TestCoverage(t *testing.T) {
	if cov, ok := (Function{Covered: 3, Total: 4}).Coverage(); !ok || cov != 0.75 {
		t.Fatalf("Coverage = %v, %v", cov, ok)
	}
	if _, ok := (Function{Total: 0}).Coverage(); ok {
		t.Fatal("Total 0 must be unmeasured")
	}
}
