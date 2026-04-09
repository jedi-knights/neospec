package reporter

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"time"

	"github.com/jedi-knights/neospec/internal/domain"
)

// JUnit writes test results in JUnit XML format.
// https://github.com/testmoapp/junitxml
// Coverage data is not included — JUnit is a test-results-only format.
type JUnit struct{}

// NewJUnit creates a JUnit reporter.
func NewJUnit() *JUnit { return &JUnit{} }

type junitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	Tests      int              `xml:"tests,attr"`
	Failures   int              `xml:"failures,attr"`
	Errors     int              `xml:"errors,attr"`
	Skipped    int              `xml:"skipped,attr"`
	Time       float64          `xml:"time,attr"`
	Timestamp  string           `xml:"timestamp,attr"`
	TestSuites []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Skipped   int             `xml:"skipped,attr"`
	Time      float64         `xml:"time,attr"`
	TestCases []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name    string        `xml:"name,attr"`
	Time    float64       `xml:"time,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
	Error   *junitError   `xml:"error,omitempty"`
	Skipped *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitError struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitSkipped struct{}

func (j *JUnit) Write(_ context.Context, w io.Writer, suite *domain.SuiteResult, _ *domain.CoverageData) error {
	pass, fail, skip, errors := suite.Counts()

	jSuite := junitTestSuite{
		Name:     "neospec",
		Tests:    len(suite.Tests),
		Failures: fail,
		Errors:   errors,
		Skipped:  skip,
		Time:     suite.Duration.Seconds(),
	}

	for _, t := range suite.Tests {
		tc := junitTestCase{
			Name: t.Name,
			Time: t.Duration.Seconds(),
		}
		switch t.Status {
		case domain.StatusFail:
			tc.Failure = &junitFailure{Message: t.Error, Text: t.Error}
		case domain.StatusError:
			tc.Error = &junitError{Message: t.Error, Text: t.Error}
		case domain.StatusSkip:
			tc.Skipped = &junitSkipped{}
		}
		jSuite.TestCases = append(jSuite.TestCases, tc)
	}

	root := junitTestSuites{
		Tests:      len(suite.Tests),
		Failures:   fail,
		Errors:     errors,
		Skipped:    skip,
		Time:       suite.Duration.Seconds(),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		TestSuites: []junitTestSuite{jSuite},
	}

	_ = pass // pass count is not a JUnit attribute at the suites level

	fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return fmt.Errorf("encoding junit XML: %w", err)
	}
	return enc.Flush()
}
