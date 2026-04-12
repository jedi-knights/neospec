package domain_test

import (
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestTestStatus_String(t *testing.T) {
	tests := []struct {
		status domain.TestStatus
		want   string
	}{
		{domain.StatusPass, "pass"},
		{domain.StatusFail, "fail"},
		{domain.StatusSkip, "skip"},
		{domain.StatusError, "error"},
		{domain.TestStatus(99), "unknown"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.status.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSuiteResult_Counts(t *testing.T) {
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{
			{Status: domain.StatusPass},
			{Status: domain.StatusPass},
			{Status: domain.StatusFail},
			{Status: domain.StatusSkip},
			{Status: domain.StatusError},
		},
	}
	pass, fail, skip, errors := suite.Counts()
	if pass != 2 {
		t.Errorf("pass = %d, want 2", pass)
	}
	if fail != 1 {
		t.Errorf("fail = %d, want 1", fail)
	}
	if skip != 1 {
		t.Errorf("skip = %d, want 1", skip)
	}
	if errors != 1 {
		t.Errorf("errors = %d, want 1", errors)
	}
}

func TestSuiteResult_Counts_Empty(t *testing.T) {
	suite := &domain.SuiteResult{}
	pass, fail, skip, errors := suite.Counts()
	if pass != 0 || fail != 0 || skip != 0 || errors != 0 {
		t.Errorf("empty suite Counts() = %d/%d/%d/%d, want 0/0/0/0", pass, fail, skip, errors)
	}
}

func TestSuiteResult_Passed(t *testing.T) {
	tests := []struct {
		name  string
		tests []domain.TestResult
		want  bool
	}{
		{
			name:  "all pass",
			tests: []domain.TestResult{{Status: domain.StatusPass}},
			want:  true,
		},
		{
			name:  "has failure",
			tests: []domain.TestResult{{Status: domain.StatusPass}, {Status: domain.StatusFail}},
			want:  false,
		},
		{
			name:  "has error",
			tests: []domain.TestResult{{Status: domain.StatusPass}, {Status: domain.StatusError}},
			want:  false,
		},
		{
			name:  "skip does not fail",
			tests: []domain.TestResult{{Status: domain.StatusSkip}},
			want:  true,
		},
		{
			name:  "empty suite",
			tests: nil,
			want:  true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := &domain.SuiteResult{Tests: tc.tests}
			if got := s.Passed(); got != tc.want {
				t.Errorf("Passed() = %v, want %v", got, tc.want)
			}
		})
	}
}
