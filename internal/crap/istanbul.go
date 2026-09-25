package crap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// istanbulLocation is a source range in an Istanbul report; lines are 1-based.
type istanbulLocation struct {
	Start struct {
		Line int `json:"line"`
	} `json:"start"`
	End struct {
		Line int `json:"line"`
	} `json:"end"`
}

type istanbulFunction struct {
	Name string           `json:"name"`
	Decl istanbulLocation `json:"decl"`
	Loc  istanbulLocation `json:"loc"`
}

type istanbulFile struct {
	StatementMap map[string]istanbulLocation `json:"statementMap"`
	FnMap        map[string]istanbulFunction `json:"fnMap"`
	S            map[string]int              `json:"s"`
}

// ParseIstanbul parses an Istanbul coverage-final.json into per-function
// Functions measured in statements. Each file's fnMap supplies the function
// boundaries (loc, the whole function body, when present, else decl) and the
// statementMap/s pair supplies execution counts; a function's coverage is the
// fraction of statements overlapping its range that executed.
//
// File paths in coverage-final.json are absolute (or tool-relative). root is
// the repo root the reports were generated in; paths under it are made
// repo-relative so they can match the changed-file set. A path not under root
// is kept verbatim; the step's completeness check then fails closed instead of
// silently scoring nothing.
func ParseIstanbul(data []byte, root string) ([]Function, error) {
	var files map[string]istanbulFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("parse coverage-final.json: %w", err)
	}
	root = normalizeReportPath(root)

	paths := make([]string, 0, len(files))
	for rawPath := range files {
		paths = append(paths, rawPath)
	}
	sort.Strings(paths)

	var fns []Function
	for _, rawPath := range paths {
		file := files[rawPath]
		path := normalizeReportPath(rawPath)
		rel := path
		if root != "" {
			if trimmed := strings.TrimPrefix(path, root); trimmed != path {
				rel = strings.TrimPrefix(trimmed, "/")
			}
		}

		fnIDs := make([]string, 0, len(file.FnMap))
		for id := range file.FnMap {
			fnIDs = append(fnIDs, id)
		}
		sort.Strings(fnIDs)

		for _, id := range fnIDs {
			fn := file.FnMap[id]
			start, end := fn.Loc.Start.Line, fn.Loc.End.Line
			if start <= 0 {
				start, end = fn.Decl.Start.Line, fn.Decl.End.Line
			}
			covered, total := statementsInRange(file, start, end)
			fns = append(fns, Function{
				Language:   LanguageTypeScript, // refined to javascript/typescript by the step
				File:       rel,
				Name:       fn.Name,
				StartLine:  start,
				EndLine:    end,
				Complexity: 1, // complexity comes from the ESLint report; see ApplyComplexity
				Covered:    covered,
				Total:      total,
				Measure:    MeasureStatement,
			})
		}
	}
	return fns, nil
}

// statementsInRange counts statements overlapping [start,end] and how many
// executed (s[id] > 0).
func statementsInRange(file istanbulFile, start, end int) (covered, total int) {
	for id, loc := range file.StatementMap {
		if loc.Start.Line <= end && loc.End.Line >= start {
			total++
			if file.S[id] > 0 {
				covered++
			}
		}
	}
	return covered, total
}
