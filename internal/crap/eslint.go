package crap

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// eslintComplexityRe matches ESLint's built-in complexity rule message:
// "Function 'foo' has a complexity of 12." (or without the quoted name when
// the function is anonymous).
var eslintComplexityRe = regexp.MustCompile(`has a complexity of (\d+)`)

type eslintMessage struct {
	RuleID  string `json:"ruleId"`
	Message string `json:"message"`
	Line    int    `json:"line"`
}

type eslintFile struct {
	FilePath string          `json:"filePath"`
	Messages []eslintMessage `json:"messages"`
}

// ComplexityReport maps a normalized file path to the cyclomatic complexity of
// each function start line, as reported by ESLint's `complexity` rule run with
// max: 1. The rule only reports functions whose complexity exceeds its max, so
// an analyzed file's entry for a function start line being absent means that
// function has complexity 1; a file absent from the whole report was not
// analyzed and must fail the step closed (see ApplyComplexity).
type ComplexityReport struct {
	byFile map[string]map[int]int
	files  map[string]bool
}

// ParseESLintComplexity parses `eslint --format json` output. root is the repo
// root the report was generated in and is stripped from file paths exactly as
// ParseIstanbul strips it, so the two reports match. Only messages whose
// ruleId is "complexity" are read; any such message whose number cannot be
// parsed fails the parse closed (the message text is ESLint's public contract,
// but a drift must never silently default a complexity).
func ParseESLintComplexity(data []byte, root string) (*ComplexityReport, error) {
	var files []eslintFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("parse eslint json output: %w", err)
	}
	root = normalizeReportPath(root)
	rep := &ComplexityReport{
		byFile: make(map[string]map[int]int),
		files:  make(map[string]bool),
	}
	for _, file := range files {
		path := normalizeReportPath(file.FilePath)
		if root != "" {
			if trimmed := strings.TrimPrefix(path, root); trimmed != path {
				path = strings.TrimPrefix(trimmed, "/")
			}
		}
		rep.files[path] = true
		for _, msg := range file.Messages {
			if msg.RuleID != "complexity" {
				continue
			}
			m := eslintComplexityRe.FindStringSubmatch(msg.Message)
			if m == nil {
				return nil, fmt.Errorf("parse eslint complexity message %q: expected \"has a complexity of N\"", msg.Message)
			}
			var n int
			if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
				return nil, fmt.Errorf("parse eslint complexity message %q: %w", msg.Message, err)
			}
			if rep.byFile[path] == nil {
				rep.byFile[path] = make(map[int]int)
			}
			rep.byFile[path][msg.Line] = n
		}
	}
	return rep, nil
}

// Complexity returns the complexity reported for (file, startLine), and false
// when the file was not analyzed at all.
func (c *ComplexityReport) Complexity(file string, startLine int) (complexity int, fileAnalyzed bool) {
	if c == nil || !c.files[file] {
		return 0, false
	}
	if n, ok := c.byFile[file][startLine]; ok {
		return n, true
	}
	return 1, true // analyzed but no entry: the rule ran at max 1, so CC == 1
}

// ApplyComplexity merges an ESLint complexity report into Istanbul functions
// (matched by file and start line), returning an error when an in-scope file
// has no complexity data at all — the file was not analyzed, so the score for
// it would be a guess, not a measurement.
func ApplyComplexity(fns []Function, rep *ComplexityReport) ([]Function, error) {
	if rep == nil {
		return nil, fmt.Errorf("no complexity report: configure eslint json or crap.command")
	}
	out := make([]Function, len(fns))
	for i, fn := range fns {
		n, analyzed := rep.Complexity(fn.File, fn.StartLine)
		if !analyzed {
			return nil, fmt.Errorf("file %q has no complexity data (eslint did not analyze it); refusing to guess its CRAP", fn.File)
		}
		fn.Complexity = n
		out[i] = fn
	}
	return out, nil
}

// LanguageFromFile returns the CRAP language identifier for a JavaScript or
// TypeScript file path, or "" when the extension is not one of them. Callers
// that need a language for an arbitrary file (crap.command entries) fall back
// to LanguageCommand themselves.
func LanguageFromFile(file string) string {
	file = strings.ToLower(file)
	switch {
	case strings.HasSuffix(file, ".ts"), strings.HasSuffix(file, ".tsx"), strings.HasSuffix(file, ".mts"), strings.HasSuffix(file, ".cts"):
		return LanguageTypeScript
	case strings.HasSuffix(file, ".js"), strings.HasSuffix(file, ".jsx"), strings.HasSuffix(file, ".mjs"), strings.HasSuffix(file, ".cjs"):
		return LanguageJavaScript
	default:
		return ""
	}
}
