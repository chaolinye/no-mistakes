package crap

import (
	"testing"
)

const radonSample = `{
  "src/orders.py": [
    {"type": "function", "name": "apply_discount", "lineno": 12, "col_offset": 0, "endline": 40, "complexity": 12, "rank": "C"},
    {"type": "class", "name": "Pricing", "lineno": 42, "col_offset": 0, "endline": 90, "complexity": 8, "rank": "B",
     "methods": [
       {"type": "method", "name": "compute", "classname": "Pricing", "lineno": 43, "col_offset": 4, "endline": 80, "complexity": 5, "rank": "B"}
     ]}
  ]
}`

const coverageSample = `{
  "files": {
    "src/orders.py": {
      "executed_lines": [12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40],
      "missing_lines": []
    }
  }
}`

func TestParsePython(t *testing.T) {
	// Coverage executes only lines 12..28 of the function range 12..40.
	cov := `{
	  "files": {
	    "src/orders.py": {
	      "executed_lines": [12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28],
	      "missing_lines": [29,30,31,32,33,34,35,36,37,38,39,40]
	    }
	  }
	}`
	fns, err := ParsePython([]byte(radonSample), []byte(cov))
	if err != nil {
		t.Fatalf("ParsePython: %v", err)
	}
	if len(fns) != 2 {
		t.Fatalf("got %d functions, want 2 (method flattened from class)", len(fns))
	}
	apply := fns[0]
	if apply.Name != "apply_discount" || apply.File != "src/orders.py" ||
		apply.StartLine != 12 || apply.EndLine != 40 || apply.Complexity != 12 {
		t.Fatalf("apply_discount = %+v", apply)
	}
	if apply.Covered != 17 || apply.Total != 29 {
		t.Fatalf("apply_discount coverage = %d/%d, want 17/29", apply.Covered, apply.Total)
	}
	if apply.Measure != MeasureLine || apply.Language != LanguagePython {
		t.Fatalf("apply_discount measure/lang = %v/%v", apply.Measure, apply.Language)
	}
	compute := fns[1]
	if compute.Name != "Pricing.compute" || compute.StartLine != 43 {
		t.Fatalf("method naming/line = %+v", compute)
	}
}

func TestParsePythonMissingCoverageFileIsZero(t *testing.T) {
	// A radon file absent from the coverage report counts as 0% covered.
	radon := `{"src/untested.py": [{"type":"function","name":"f","lineno":1,"col_offset":0,"endline":5,"complexity":4,"rank":"B"}]}`
	cov := `{"files": {"src/other.py": {"executed_lines": [1], "missing_lines": []}}}`
	fns, err := ParsePython([]byte(radon), []byte(cov))
	if err != nil {
		t.Fatalf("ParsePython: %v", err)
	}
	if len(fns) != 1 || fns[0].Covered != 0 || fns[0].Total != 1 {
		t.Fatalf("unexpected: %+v", fns)
	}
	if cov, ok := fns[0].Coverage(); !ok || cov != 0 {
		t.Fatalf("coverage = %v,%v want 0,true", cov, ok)
	}
}

func TestParsePythonMalformed(t *testing.T) {
	if _, err := ParsePython([]byte(`{`), []byte(coverageSample)); err == nil {
		t.Fatal("malformed radon must fail")
	}
	if _, err := ParsePython([]byte(radonSample), []byte(`{`)); err == nil {
		t.Fatal("malformed coverage must fail")
	}
}

func TestParsePythonFullCoverage(t *testing.T) {
	fns, err := ParsePython([]byte(radonSample), []byte(coverageSample))
	if err != nil {
		t.Fatalf("ParsePython: %v", err)
	}
	apply := fns[0]
	if apply.Covered != 29 || apply.Total != 29 {
		t.Fatalf("full coverage = %d/%d, want 29/29", apply.Covered, apply.Total)
	}
}
