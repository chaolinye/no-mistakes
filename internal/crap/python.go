package crap

import (
	"encoding/json"
	"fmt"
)

// radonBlock is one block in `radon cc -j` output. Classes carry their
// methods nested, so the type recurses.
type radonBlock struct {
	Type       string       `json:"type"`
	Name       string       `json:"name"`
	LineNo     int          `json:"lineno"`
	EndLine    int          `json:"endline"`
	Complexity int          `json:"complexity"`
	ClassName  string       `json:"classname"`
	Methods    []radonBlock `json:"methods"`
}

// coveragePyFile is one file entry in `coverage json` output.
type coveragePyFile struct {
	ExecutedLines []int `json:"executed_lines"`
	MissingLines  []int `json:"missing_lines"`
}

type coveragePyReport struct {
	Files map[string]coveragePyFile `json:"files"`
}

// ParsePython combines `radon cc -j` complexity blocks with `coverage json`
// line data into per-function Functions measured in lines.
//
// A file absent from the coverage report counts as 0% covered (Covered 0,
// Total 1): that is the change-risk reading the metric exists for — code the
// test suite never imports is riskiest of all — and matches how CRAP tools
// treat missing coverage. A file present in the report whose function range
// contains no executable lines is unmeasured (Total 0) and is reported rather
// than gated.
func ParsePython(radonJSON, coverageJSON []byte) ([]Function, error) {
	var blocks map[string][]radonBlock
	if err := json.Unmarshal(radonJSON, &blocks); err != nil {
		return nil, fmt.Errorf("parse radon cc -j output: %w", err)
	}
	var cov coveragePyReport
	if err := json.Unmarshal(coverageJSON, &cov); err != nil {
		return nil, fmt.Errorf("parse coverage.py json report: %w", err)
	}

	var fns []Function
	for file, entries := range blocks {
		file = normalizeReportPath(file)
		fileData, inReport := cov.Files[file]
		executed := make(map[int]bool)
		all := make(map[int]bool)
		for _, line := range fileData.ExecutedLines {
			executed[line] = true
			all[line] = true
		}
		for _, line := range fileData.MissingLines {
			all[line] = true
		}
		for _, entry := range entries {
			fns = append(fns, flattenRadonBlocks(file, entry, executed, all, inReport)...)
		}
	}
	return fns, nil
}

// flattenRadonBlocks walks a radon block tree, emitting only function and
// method blocks. Classes are containers; their methods are already emitted as
// nested blocks and counted with the class's source range.
func flattenRadonBlocks(file string, block radonBlock, executed, all map[int]bool, fileInReport bool) []Function {
	var out []Function
	if block.Type == "function" || block.Type == "method" {
		covered, total := linesInRange(executed, all, block.LineNo, block.EndLine)
		if !fileInReport && total == 0 {
			// File was never exercised at all: 0% covered (marker total 1).
			total, covered = 1, 0
		}
		name := block.Name
		if block.Type == "method" && block.ClassName != "" {
			name = block.ClassName + "." + block.Name
		}
		out = append(out, Function{
			Language:   LanguagePython,
			File:       file,
			Name:       name,
			StartLine:  block.LineNo,
			EndLine:    block.EndLine,
			Complexity: block.Complexity,
			Covered:    covered,
			Total:      total,
			Measure:    MeasureLine,
		})
	}
	for _, method := range block.Methods {
		out = append(out, flattenRadonBlocks(file, method, executed, all, fileInReport)...)
	}
	return out
}

// linesInRange counts how many of the file's executable lines fall inside
// [start,end] and how many of those were executed.
func linesInRange(executed, all map[int]bool, start, end int) (covered, total int) {
	if start < 1 {
		start = 1
	}
	if end < start {
		return 0, 0
	}
	for line := start; line <= end; line++ {
		if all[line] {
			total++
			if executed[line] {
				covered++
			}
		}
	}
	return covered, total
}
