package crap

import "testing"

func TestParseDiffHunks(t *testing.T) {
	diff := `diff --git a/src/a.py b/src/a.py
index abc..def 100644
--- a/src/a.py
+++ b/src/a.py
@@ -10,0 +11,3 @@
+def new_fn():
+    pass
+    pass
diff --git a/src/b.ts b/src/b.ts
index 111..222 100644
--- a/src/b.ts
+++ b/src/b.ts
@@ -5,2 +5,3 @@
 context
+added
+added2
diff --git a/src/deleted.txt b/src/deleted.txt
deleted file mode 100644
index 333..0000000
--- a/src/deleted.txt
+++ /dev/null
@@ -1,4 +0,0 @@
-removed
diff --git a/src/renamed.txt b/src/renamed.txt
similarity index 100%
rename from src/old.txt
rename to src/new.txt
`
	changed, err := ParseDiffHunks(diff)
	if err != nil {
		t.Fatalf("ParseDiffHunks: %v", err)
	}

	if got := changed["src/a.py"]; len(got) != 1 || got[0] != (LineRange{Start: 11, End: 13}) {
		t.Fatalf("src/a.py ranges = %v, want [{11 13}]", got)
	}
	if got := changed["src/b.ts"]; len(got) != 1 || got[0] != (LineRange{Start: 5, End: 7}) {
		t.Fatalf("src/b.ts ranges = %v, want [{5 7}]", got)
	}
	// Deleted-only hunks contribute nothing (no "+" side).
	if _, ok := changed["src/deleted.txt"]; ok {
		t.Fatal("deleted file must not contribute ranges")
	}
	// Rename resolves to the new path.
	if _, ok := changed["src/new.txt"]; ok {
		t.Fatal("pure rename contributes no ranges")
	}
}

func TestChangedLinesInScope(t *testing.T) {
	changed := ChangedLines{
		"src/a.py": {{Start: 11, End: 13}},
	}

	inside := Function{File: "src/a.py", StartLine: 12, EndLine: 20}
	if !changed.InScope(inside) {
		t.Fatal("function overlapping a changed line must be in scope")
	}
	outside := Function{File: "src/a.py", StartLine: 50, EndLine: 60}
	if changed.InScope(outside) {
		t.Fatal("function not overlapping must be out of scope")
	}
	unanchored := Function{File: "src/a.py", StartLine: 0, EndLine: 0}
	if changed.InScope(unanchored) {
		t.Fatal("function without a source line must be out of scope")
	}
	otherFile := Function{File: "src/b.py", StartLine: 12, EndLine: 20}
	if changed.InScope(otherFile) {
		t.Fatal("function in an unchanged file must be out of scope")
	}
}

func TestChangedLinesNilMeansNothingInScope(t *testing.T) {
	var changed ChangedLines // nil: changed-scope mode with an empty diff
	f := Function{File: "src/a.py", StartLine: 12, EndLine: 20}
	if changed.InScope(f) {
		t.Fatal("nil ChangedLines means nothing is in changed scope; scope: all skips InScope entirely")
	}
}
