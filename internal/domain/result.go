package domain

import "time"

// TestStatus is the outcome of a single test case.
type TestStatus int

// Test outcome constants.
const (
	StatusPass TestStatus = iota
	StatusFail
	StatusSkip
	StatusError
)

func (s TestStatus) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// TestResult holds the outcome of one `it` block.
type TestResult struct {
	// Name is the full path of describe/it labels, e.g. "mymodule > behaves correctly".
	Name     string
	Status   TestStatus
	Duration time.Duration
	// Output is any text the test wrote to stdout.
	Output string
	// Error is the failure or error message, empty when Status == StatusPass or StatusSkip.
	Error string
}

// SuiteResult aggregates all test results from a run.
type SuiteResult struct {
	Tests    []TestResult
	Duration time.Duration
}

// Counts returns the pass, fail, skip, and error counts.
func (s *SuiteResult) Counts() (pass, fail, skip, errors int) {
	for _, t := range s.Tests {
		switch t.Status {
		case StatusPass:
			pass++
		case StatusFail:
			fail++
		case StatusSkip:
			skip++
		case StatusError:
			errors++
		}
	}
	return
}

// Passed reports whether the suite has no failures or errors.
func (s *SuiteResult) Passed() bool {
	_, fail, _, errors := s.Counts()
	return fail == 0 && errors == 0
}
