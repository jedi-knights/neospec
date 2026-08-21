package reporter

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/jedi-knights/neospec/internal/domain"
)

// Cobertura writes coverage data in Cobertura XML format.
// https://cobertura.github.io/cobertura/
type Cobertura struct{}

// NewCobertura creates a Cobertura reporter.
func NewCobertura() *Cobertura { return &Cobertura{} }

// coberturaXML is the root element of the Cobertura XML report. Branch
// counters mirror the line counters so downstream tools that render
// combined line + branch coverage (Jenkins Cobertura plugin, ReportGenerator,
// GitHub Codecov) show non-zero branch data.
type coberturaXML struct {
	XMLName         xml.Name          `xml:"coverage"`
	Version         string            `xml:"version,attr"`
	Timestamp       int64             `xml:"timestamp,attr"`
	LinesValid      int               `xml:"lines-valid,attr"`
	LinesCovered    int               `xml:"lines-covered,attr"`
	LineRate        float64           `xml:"line-rate,attr"`
	BranchesValid   int               `xml:"branches-valid,attr"`
	BranchesCovered int               `xml:"branches-covered,attr"`
	BranchRate      float64           `xml:"branch-rate,attr"`
	Packages        coberturaPackages `xml:"packages"`
}

type coberturaPackages struct {
	Packages []coberturaPackage `xml:"package"`
}

type coberturaPackage struct {
	Name       string           `xml:"name,attr"`
	LineRate   float64          `xml:"line-rate,attr"`
	BranchRate float64          `xml:"branch-rate,attr"`
	Classes    coberturaClasses `xml:"classes"`
}

type coberturaClasses struct {
	Classes []coberturaClass `xml:"class"`
}

type coberturaClass struct {
	Name       string         `xml:"name,attr"`
	Filename   string         `xml:"filename,attr"`
	LineRate   float64        `xml:"line-rate,attr"`
	BranchRate float64        `xml:"branch-rate,attr"`
	Lines      coberturaLines `xml:"lines"`
}

type coberturaLines struct {
	Lines []coberturaLine `xml:"line"`
}

// coberturaLine represents one source line. Branch and ConditionCoverage
// use omitempty so plain (non-branch) lines emit no branch attributes at
// all, matching the convention used by cobertura-py and gocover-cobertura.
type coberturaLine struct {
	Number            int                  `xml:"number,attr"`
	Hits              int                  `xml:"hits,attr"`
	Branch            bool                 `xml:"branch,attr,omitempty"`
	ConditionCoverage string               `xml:"condition-coverage,attr,omitempty"`
	Conditions        *coberturaConditions `xml:"conditions,omitempty"`
}

type coberturaConditions struct {
	Conditions []coberturaCondition `xml:"condition"`
}

// coberturaCondition describes one branch arm. Type is "jump" for
// standard boolean control-flow — cobertura's DTD accepts other values
// (e.g., "switch") but every consumer we care about renders "jump".
type coberturaCondition struct {
	Number   int    `xml:"number,attr"`
	Type     string `xml:"type,attr"`
	Coverage string `xml:"coverage,attr"`
}

// Write serializes cov as a Cobertura XML report.
func (c *Cobertura) Write(_ context.Context, w io.Writer, _ *domain.SuiteResult, cov *domain.CoverageData) error {
	if cov == nil {
		cov = &domain.CoverageData{}
	}

	report := coberturaXML{
		Version:         "neospec-1.0",
		Timestamp:       time.Now().Unix(),
		LinesValid:      cov.TotalLines(),
		LinesCovered:    cov.HitLines(),
		LineRate:        rate(cov.HitLines(), cov.TotalLines()),
		BranchesValid:   cov.TotalBranches(),
		BranchesCovered: cov.HitBranches(),
		BranchRate:      rate(cov.HitBranches(), cov.TotalBranches()),
	}

	for _, file := range cov.Files {
		report.Packages.Packages = append(report.Packages.Packages, buildPackage(file))
	}

	fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encoding cobertura XML: %w", err)
	}
	return enc.Flush()
}

// buildPackage produces the <package>/<class>/<lines> subtree for one file.
// Neospec has no true package concept; every file lands in a single "."
// package to keep the DTD happy and per-file class rows readable.
func buildPackage(file *domain.FileCoverage) coberturaPackage {
	lineRate := rate(file.HitLines(), file.TotalLines())
	branchRate := rate(file.HitBranches(), file.TotalBranches())

	cls := coberturaClass{
		Name:       file.Path,
		Filename:   file.Path,
		LineRate:   lineRate,
		BranchRate: branchRate,
	}
	cls.Lines.Lines = buildLines(file)

	pkg := coberturaPackage{
		Name:       ".",
		LineRate:   lineRate,
		BranchRate: branchRate,
	}
	pkg.Classes.Classes = append(pkg.Classes.Classes, cls)
	return pkg
}

// buildLines produces the sorted <line> list, attaching branch attributes
// to any line that has a corresponding branch record.
func buildLines(file *domain.FileCoverage) []coberturaLine {
	lineNums := make([]int, 0, len(file.Lines))
	for ln := range file.Lines {
		lineNums = append(lineNums, ln)
	}
	sort.Ints(lineNums)

	byLine := branchesByLine(file.Branches)

	lines := make([]coberturaLine, 0, len(lineNums))
	for _, ln := range lineNums {
		line := coberturaLine{Number: ln, Hits: file.Lines[ln]}
		if bs := byLine[ln]; len(bs) > 0 {
			line.Branch = true
			line.ConditionCoverage = conditionCoverage(bs)
			line.Conditions = buildConditions(bs)
		}
		lines = append(lines, line)
	}
	return lines
}

// branchesByLine groups branch records by their source line so a single
// pass over line numbers can attach the branch info in one lookup.
func branchesByLine(branches []domain.BranchCoverage) map[int][]domain.BranchCoverage {
	out := make(map[int][]domain.BranchCoverage, len(branches))
	for _, b := range branches {
		out[b.Line] = append(out[b.Line], b)
	}
	return out
}

// conditionCoverage renders the human-readable "N% (hit/total)" string
// that most cobertura consumers display in a line-level tooltip.
func conditionCoverage(branches []domain.BranchCoverage) string {
	total, hit := 0, 0
	for _, b := range branches {
		for _, arm := range b.Arms {
			total++
			if arm.Taken > 0 {
				hit++
			}
		}
	}
	if total == 0 {
		return ""
	}
	pct := 100 * hit / total
	return fmt.Sprintf("%d%% (%d/%d)", pct, hit, total)
}

// buildConditions emits one <condition> per arm across the given branches.
// Arms with Taken > 0 render as "100%", the rest as "0%" — cobertura has
// no representation for "unknown", so an arm we cannot score gets counted
// as a miss (same treatment as LCOV's `-`, which excludes unknown from BRH
// but includes it in BRF).
func buildConditions(branches []domain.BranchCoverage) *coberturaConditions {
	conds := &coberturaConditions{}
	idx := 0
	for _, b := range branches {
		for _, arm := range b.Arms {
			coverage := "0%"
			if arm.Taken > 0 {
				coverage = "100%"
			}
			conds.Conditions = append(conds.Conditions, coberturaCondition{
				Number: idx, Type: "jump", Coverage: coverage,
			})
			idx++
		}
	}
	return conds
}

// rate returns hit/total as a fraction, or 0 when total is zero — matches
// cobertura's convention of "no coverage measured" rather than NaN.
func rate(hit, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(hit) / float64(total)
}
