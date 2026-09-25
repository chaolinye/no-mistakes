package crap

import "testing"

// istanbulSample mirrors what jest/vitest/c8 emit: fnMap entries carry decl and
// loc; statementMap + s supply execution counts.
const istanbulSample = `{
  "/repo/src/pricing.ts": {
    "path": "/repo/src/pricing.ts",
    "statementMap": {
      "0": {"start": {"line": 2, "column": 4}, "end": {"line": 2, "column": 30}},
      "1": {"start": {"line": 3, "column": 4}, "end": {"line": 3, "column": 20}},
      "2": {"start": {"line": 4, "column": 4}, "end": {"line": 4, "column": 22}},
      "3": {"start": {"line": 5, "column": 4}, "end": {"line": 5, "column": 26}},
      "4": {"start": {"line": 6, "column": 4}, "end": {"line": 6, "column": 18}},
      "5": {"start": {"line": 9, "column": 4}, "end": {"line": 9, "column": 24}}
    },
    "fnMap": {
      "0": {
        "name": "pricingFor",
        "decl": {"start": {"line": 1, "column": 0}, "end": {"line": 1, "column": 14}},
        "loc": {"start": {"line": 1, "column": 0}, "end": {"line": 7, "column": 1}},
        "line": 1
      },
      "1": {
        "name": "helper",
        "decl": {"start": {"line": 8, "column": 0}, "end": {"line": 8, "column": 10}},
        "loc": {"start": {"line": 8, "column": 0}, "end": {"line": 10, "column": 1}},
        "line": 8
      }
    },
    "s": {"0": 1, "1": 1, "2": 0, "3": 1, "4": 0, "5": 1},
    "f": {"0": 1, "1": 1},
    "b": {}
  }
}`

func TestParseIstanbul(t *testing.T) {
	fns, err := ParseIstanbul([]byte(istanbulSample), "/repo")
	if err != nil {
		t.Fatalf("ParseIstanbul: %v", err)
	}
	if len(fns) != 2 {
		t.Fatalf("got %d functions, want 2", len(fns))
	}

	pricing := fns[0]
	if pricing.File != "src/pricing.ts" || pricing.Name != "pricingFor" ||
		pricing.StartLine != 1 || pricing.EndLine != 7 {
		t.Fatalf("pricingFor = %+v", pricing)
	}
	// Statements 0..4 overlap range 1..7; executed are 0,1,3 -> 3/5.
	if pricing.Covered != 3 || pricing.Total != 5 || pricing.Measure != MeasureStatement {
		t.Fatalf("pricingFor coverage = %d/%d %v, want 3/5 statement", pricing.Covered, pricing.Total, pricing.Measure)
	}
	// Parser defaults complexity to 1 until ApplyComplexity fills it.
	if pricing.Complexity != 1 {
		t.Fatalf("pricingFor complexity must default 1, got %d", pricing.Complexity)
	}

	helper := fns[1]
	if helper.StartLine != 8 || helper.EndLine != 10 {
		t.Fatalf("helper = %+v", helper)
	}
	if helper.Covered != 1 || helper.Total != 1 {
		t.Fatalf("helper coverage = %d/%d, want 1/1", helper.Covered, helper.Total)
	}
}

func TestParseIstanbulPathNotUnderRoot(t *testing.T) {
	fns, err := ParseIstanbul([]byte(istanbulSample), "/other")
	if err != nil {
		t.Fatalf("ParseIstanbul: %v", err)
	}
	if len(fns) != 2 {
		t.Fatal("expected 2 functions")
	}
	// Path not under root is kept verbatim so the completeness check can fail
	// closed instead of scoring nothing.
	if fns[0].File != "/repo/src/pricing.ts" {
		t.Fatalf("path kept verbatim = %q", fns[0].File)
	}
}

func TestParseIstanbulMalformed(t *testing.T) {
	if _, err := ParseIstanbul([]byte(`{`), "/repo"); err == nil {
		t.Fatal("malformed coverage-final.json must fail")
	}
}
