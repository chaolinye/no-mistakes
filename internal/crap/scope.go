package crap

import (
	"fmt"
	"regexp"
	"strconv"
)

// LineRange is an inclusive line range in one file.
type LineRange struct {
	Start int
	End   int
}

// ChangedLines maps a repository-relative file path to the line ranges that
// were added or modified in the diff (the "+" side of `git diff -U0`).
type ChangedLines map[string][]LineRange

// diffHunkRe matches a unified-diff hunk header; the new-file side is groups 3
// and 4. `git diff -U0` produces no context, so the new-file range is exactly
// the added/modified lines.
var diffHunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParseDiffHunks parses `git diff -U0` output into ChangedLines. Only the
// new-file ("+") side is kept, so deleted-only hunks contribute nothing.
// Binary diffs and "no newline" markers are ignored.
func ParseDiffHunks(diff string) (ChangedLines, error) {
	changed := make(ChangedLines)
	var current string
	for _, line := range splitLines(diff) {
		if m := diffHunkRe.FindStringSubmatch(line); m != nil {
			start, err := strconv.Atoi(m[3])
			if err != nil {
				return nil, fmt.Errorf("parse diff hunk %q: %w", line, err)
			}
			count := 1
			if m[4] != "" {
				count, err = strconv.Atoi(m[4])
				if err != nil {
					return nil, fmt.Errorf("parse diff hunk %q: %w", line, err)
				}
			}
			if count > 0 {
				changed[current] = append(changed[current], LineRange{Start: start, End: start + count - 1})
			}
			continue
		}
		if newPath, ok := diffPath(line); ok {
			current = newPath
		}
	}
	return changed, nil
}

// diffPath extracts the new-side path from a `diff --git a/old b/new` line.
// Renames are normalized to the new path.
func diffPath(line string) (string, bool) {
	const prefix = "diff --git "
	if len(line) < len(prefix) || line[:len(prefix)] != prefix {
		return "", false
	}
	rest := line[len(prefix):]
	// Split on " b/" at the last occurrence: paths can contain " b/" inside.
	idx := lastIndex(rest, " b/")
	if idx < 0 {
		return "", false
	}
	newPath := rest[idx+len(" b/"):]
	return normalizeReportPath(newPath), true
}

// lastIndex returns the byte index of the last occurrence of sub in s, or -1.
func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// splitLines splits on "\n" and drops the trailing empty element.
func splitLines(s string) []string {
	lines := make([]string, 0, 64)
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			if i > start {
				lines = append(lines, s[start:i])
			}
			start = i + 1
		}
	}
	return lines
}

// InScope reports whether a function's line range intersects any changed range
// in its file. Functions the report could not anchor to a source line
// (StartLine 0) are out of scope and reported via the caller's unmeasured
// accounting. When changed is nil (scope: all), every function is in scope.
func (c ChangedLines) InScope(fn Function) bool {
	if fn.StartLine < 1 {
		return false
	}
	ranges, ok := c[fn.File]
	if !ok {
		return false
	}
	for _, r := range ranges {
		if fn.StartLine <= r.End && fn.EndLine >= r.Start {
			return true
		}
	}
	return false
}
