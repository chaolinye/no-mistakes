package crap

import "testing"

const eslintSample = `[
  {
    "filePath": "/repo/src/pricing.ts",
    "messages": [
      {"ruleId": "complexity", "severity": 2, "message": "Function 'pricingFor' has a complexity of 12.", "line": 1, "column": 0},
      {"ruleId": "complexity", "severity": 2, "message": "Function 'helper' has a complexity of 3.", "line": 8, "column": 0},
      {"ruleId": "no-unused-vars", "severity": 2, "message": "'x' is assigned a value but never used.", "line": 9, "column": 6}
    ]
  }
]`

func TestParseESLintComplexity(t *testing.T) {
	rep, err := ParseESLintComplexity([]byte(eslintSample), "/repo")
	if err != nil {
		t.Fatalf("ParseESLintComplexity: %v", err)
	}
	n, analyzed := rep.Complexity("src/pricing.ts", 1)
	if !analyzed || n != 12 {
		t.Fatalf("pricingFor complexity = %d (%v), want 12 true", n, analyzed)
	}
	n, _ = rep.Complexity("src/pricing.ts", 8)
	if n != 3 {
		t.Fatalf("helper complexity = %d, want 3", n)
	}
	// Analyzed file, no entry for this start line -> CC 1 (rule ran at max 1).
	n, analyzed = rep.Complexity("src/pricing.ts", 100)
	if !analyzed || n != 1 {
		t.Fatalf("unreported line = %d (%v), want 1 true", n, analyzed)
	}
	// Unanalyzed file -> not analyzed.
	if _, analyzed = rep.Complexity("src/other.ts", 1); analyzed {
		t.Fatal("unanalyzed file must report fileAnalyzed false")
	}
}

func TestParseESLintComplexityUnparseableFails(t *testing.T) {
	bad := `[{"filePath": "/r/a.ts", "messages": [{"ruleId": "complexity", "message": "Function 'x' got a score of 5.", "line": 1}]}]`
	if _, err := ParseESLintComplexity([]byte(bad), "/r"); err == nil {
		t.Fatal("an unparseable complexity message must fail closed")
	}
}

func TestApplyComplexity(t *testing.T) {
	fns, err := ParseIstanbul([]byte(istanbulSample), "/repo")
	if err != nil {
		t.Fatalf("ParseIstanbul: %v", err)
	}
	rep, err := ParseESLintComplexity([]byte(eslintSample), "/repo")
	if err != nil {
		t.Fatalf("ParseESLintComplexity: %v", err)
	}
	merged, err := ApplyComplexity(fns, rep)
	if err != nil {
		t.Fatalf("ApplyComplexity: %v", err)
	}
	if merged[0].Complexity != 12 || merged[1].Complexity != 3 {
		t.Fatalf("merged complexities = %d, %d; want 12, 3", merged[0].Complexity, merged[1].Complexity)
	}
}

func TestApplyComplexityMissingFileFails(t *testing.T) {
	fns := []Function{{Language: LanguageTypeScript, File: "src/never/analyzed.ts", StartLine: 1}}
	rep, err := ParseESLintComplexity([]byte(eslintSample), "/repo")
	if err != nil {
		t.Fatalf("ParseESLintComplexity: %v", err)
	}
	if _, err := ApplyComplexity(fns, rep); err == nil {
		t.Fatal("an unanalyzed in-scope file must fail closed")
	}
}

func TestLanguageFromFile(t *testing.T) {
	cases := map[string]string{
		"src/x.ts":   LanguageTypeScript,
		"src/x.tsx":  LanguageTypeScript,
		"src/x.mts":  LanguageTypeScript,
		"src/x.cts":  LanguageTypeScript,
		"src/x.js":   LanguageJavaScript,
		"src/x.jsx":  LanguageJavaScript,
		"src/x.mjs":  LanguageJavaScript,
		"src/x.cjs":  LanguageJavaScript,
		"src/x.yaml": "", // unknown extension has no language
	}
	for file, want := range cases {
		if got := LanguageFromFile(file); got != want {
			t.Errorf("LanguageFromFile(%q) = %q, want %q", file, got, want)
		}
	}
}
