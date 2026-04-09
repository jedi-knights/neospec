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

// coberturaXML is the root element of the Cobertura XML report.
type coberturaXML struct {
	XMLName      xml.Name          `xml:"coverage"`
	Version      string            `xml:"version,attr"`
	Timestamp    int64             `xml:"timestamp,attr"`
	LinesValid   int               `xml:"lines-valid,attr"`
	LinesCovered int               `xml:"lines-covered,attr"`
	LineRate     float64           `xml:"line-rate,attr"`
	Packages     coberturaPackages `xml:"packages"`
}

type coberturaPackages struct {
	Packages []coberturaPackage `xml:"package"`
}

type coberturaPackage struct {
	Name     string           `xml:"name,attr"`
	LineRate float64          `xml:"line-rate,attr"`
	Classes  coberturaClasses `xml:"classes"`
}

type coberturaClasses struct {
	Classes []coberturaClass `xml:"class"`
}

type coberturaClass struct {
	Name     string         `xml:"name,attr"`
	Filename string         `xml:"filename,attr"`
	LineRate float64        `xml:"line-rate,attr"`
	Lines    coberturaLines `xml:"lines"`
}

type coberturaLines struct {
	Lines []coberturaLine `xml:"line"`
}

type coberturaLine struct {
	Number int `xml:"number,attr"`
	Hits   int `xml:"hits,attr"`
}

func (c *Cobertura) Write(_ context.Context, w io.Writer, _ *domain.SuiteResult, cov *domain.CoverageData) error {
	if cov == nil {
		cov = &domain.CoverageData{}
	}

	lineRate := 0.0
	if cov.TotalLines() > 0 {
		lineRate = float64(cov.HitLines()) / float64(cov.TotalLines())
	}

	report := coberturaXML{
		Version:      "neospec-1.0",
		Timestamp:    time.Now().Unix(),
		LinesValid:   cov.TotalLines(),
		LinesCovered: cov.HitLines(),
		LineRate:     lineRate,
	}

	for _, file := range cov.Files {
		lineRate := 0.0
		if file.TotalLines() > 0 {
			lineRate = float64(file.HitLines()) / float64(file.TotalLines())
		}

		lines := make([]int, 0, len(file.Lines))
		for ln := range file.Lines {
			lines = append(lines, ln)
		}
		sort.Ints(lines)

		cls := coberturaClass{
			Name:     file.Path,
			Filename: file.Path,
			LineRate: lineRate,
		}
		for _, ln := range lines {
			cls.Lines.Lines = append(cls.Lines.Lines, coberturaLine{
				Number: ln,
				Hits:   file.Lines[ln],
			})
		}

		pkg := coberturaPackage{
			Name:     ".",
			LineRate: lineRate,
		}
		pkg.Classes.Classes = append(pkg.Classes.Classes, cls)
		report.Packages.Packages = append(report.Packages.Packages, pkg)
	}

	fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encoding cobertura XML: %w", err)
	}
	return enc.Flush()
}
