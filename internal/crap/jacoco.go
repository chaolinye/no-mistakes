package crap

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// jacocoXML is the subset of the JaCoCo XML report the parser reads.
type jacocoXML struct {
	Packages []jacocoPackage `xml:"package"`
}

type jacocoPackage struct {
	Name  string      `xml:"name,attr"`
	Class []jacocoCls `xml:"class"`
}

type jacocoCls struct {
	Name           string         `xml:"name,attr"`
	SourceFilename string         `xml:"sourcefilename,attr"`
	Method         []jacocoMethod `xml:"method"`
}

type jacocoMethod struct {
	Name string        `xml:"name,attr"`
	Line int           `xml:"line,attr"`
	Cnt  []jacocoCount `xml:"counter"`
}

type jacocoCount struct {
	Type    string `xml:"type,attr"`
	Missed  int    `xml:"missed,attr"`
	Covered int    `xml:"covered,attr"`
}

// ParseJacoco parses a JaCoCo XML report into per-method Functions. JaCoCo
// computes cyclomatic complexity per non-abstract method (the COMPLEXITY
// counter), so a single report supplies both sides of the CRAP formula. Branch
// coverage is preferred; when a method carries no BRANCH counter (e.g. a
// method with no decision points) LINE is used. Abstract methods (no
// COMPLEXITY counter, nothing to execute) are skipped. Methods compiled
// without debug info have no line attribute and are emitted with StartLine 0,
// which the scope filter reports as unmeasured rather than silently dropping.
func ParseJacoco(data []byte) ([]Function, error) {
	var report jacocoXML
	if err := xml.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse jacoco.xml: %w", err)
	}
	var fns []Function
	for _, pkg := range report.Packages {
		for _, cls := range pkg.Class {
			if cls.SourceFilename == "" {
				continue
			}
			file := normalizeReportPath(pkg.Name + "/" + cls.SourceFilename)
			for _, m := range cls.Method {
				complexity := -1
				branchMissed, branchCovered := -1, -1
				lineMissed, lineCovered := -1, -1
				for _, c := range m.Cnt {
					switch c.Type {
					case "COMPLEXITY":
						complexity = c.Missed + c.Covered
					case "BRANCH":
						branchMissed, branchCovered = c.Missed, c.Covered
					case "LINE":
						lineMissed, lineCovered = c.Missed, c.Covered
					}
				}
				if complexity <= 0 {
					continue // abstract method or no executable body
				}
				measure, covered, total := MeasureLine, 0, 0
				if branchMissed >= 0 || branchCovered >= 0 {
					measure, covered, total = MeasureBranch, branchCovered, branchMissed+branchCovered
				} else if lineMissed >= 0 || lineCovered >= 0 {
					measure, covered, total = MeasureLine, lineCovered, lineMissed+lineCovered
				}
				fns = append(fns, Function{
					Language:   LanguageJava,
					File:       file,
					Name:       m.Name,
					StartLine:  m.Line,
					Complexity: complexity,
					Covered:    covered,
					Total:      total,
					Measure:    measure,
				})
			}
		}
	}
	return fns, nil
}

// normalizeReportPath converts a report path to a "/"-separated key so paths
// produced on Windows and by different tools compare equal.
func normalizeReportPath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
