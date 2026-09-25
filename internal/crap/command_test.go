package crap

import "testing"

func TestParseCommandJSON(t *testing.T) {
	payload := `{
	  "functions": [
	    {"file": "src/billing/pricing.ts", "name": "pricingFor", "start_line": 42, "end_line": 118,
	     "complexity": 12, "covered": 27, "total": 60, "measure": "statement"}
	  ]
	}`
	fns, err := ParseCommandJSON([]byte(payload))
	if err != nil {
		t.Fatalf("ParseCommandJSON: %v", err)
	}
	if len(fns) != 1 {
		t.Fatalf("got %d functions, want 1", len(fns))
	}
	f := fns[0]
	if f.Language != LanguageCommand || f.File != "src/billing/pricing.ts" ||
		f.StartLine != 42 || f.Complexity != 12 || f.Covered != 27 || f.Total != 60 ||
		f.Measure != MeasureStatement {
		t.Fatalf("parsed = %+v", f)
	}
}

func TestParseCommandJSONRejectsBadEntry(t *testing.T) {
	for name, payload := range map[string]string{
		"empty file":      `{"functions": [{"name": "x", "complexity": 2, "measure": "line"}]}`,
		"zero complexity": `{"functions": [{"file": "a.ts", "complexity": 0, "measure": "line"}]}`,
		"bad measure":     `{"functions": [{"file": "a.ts", "complexity": 2, "measure": "branches"}]}`,
		"missing measure": `{"functions": [{"file": "a.ts", "complexity": 2}]}`,
		"malformed":       `{`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCommandJSON([]byte(payload)); err == nil {
				t.Fatalf("payload must fail closed: %s", payload)
			}
		})
	}
}

func TestParseCommandJSONUnmeasured(t *testing.T) {
	payload := `{"functions": [{"file": "a.ts", "name": "f", "complexity": 3, "covered": 0, "total": 0, "measure": "line"}]}`
	fns, err := ParseCommandJSON([]byte(payload))
	if err != nil {
		t.Fatalf("ParseCommandJSON: %v", err)
	}
	if _, ok := fns[0].Coverage(); ok {
		t.Fatal("total 0 must be unmeasured")
	}
}
