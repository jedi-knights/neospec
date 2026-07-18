package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMatrixResult_Passed(t *testing.T) {
	cases := []struct {
		name string
		r    MatrixResult
		want bool
	}{
		{"zero-exit no-error", MatrixResult{ExitCode: 0}, true},
		{"non-zero exit", MatrixResult{ExitCode: 1}, false},
		{"provisioning error", MatrixResult{Err: errors.New("download failed")}, false},
		{"error trumps zero exit", MatrixResult{ExitCode: 0, Err: errors.New("x")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Passed(); got != tc.want {
				t.Errorf("Passed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExecMatrix_Counts(t *testing.T) {
	m := ExecMatrix{
		Results: []MatrixResult{
			{Version: Version{Tag: "stable"}, ExitCode: 0},
			{Version: Version{Tag: "nightly"}, ExitCode: 1},
			{Version: Version{Tag: "v0.10.4"}, Err: errors.New("no binary")},
		},
	}
	if got := m.PassedCount(); got != 1 {
		t.Errorf("PassedCount = %d, want 1", got)
	}
	if got := m.FailedCount(); got != 2 {
		t.Errorf("FailedCount = %d, want 2", got)
	}
	if m.Passed() {
		t.Errorf("Passed() = true, want false")
	}
}

func TestExecMatrix_AllPassed(t *testing.T) {
	m := ExecMatrix{
		Results: []MatrixResult{
			{Version: Version{Tag: "stable"}, ExitCode: 0},
			{Version: Version{Tag: "nightly"}, ExitCode: 0},
		},
	}
	if !m.Passed() {
		t.Errorf("Passed() = false, want true")
	}
}

func TestExecMatrix_EmptyIsFailed(t *testing.T) {
	m := ExecMatrix{}
	if m.Passed() {
		t.Errorf("empty matrix.Passed() = true, want false (silent-noop guard)")
	}
	if m.PassedCount() != 0 || m.FailedCount() != 0 {
		t.Errorf("empty matrix counts = %d/%d, want 0/0", m.PassedCount(), m.FailedCount())
	}
}

func TestExecMatrix_FailedVersions(t *testing.T) {
	m := ExecMatrix{
		Results: []MatrixResult{
			{Version: Version{Tag: "stable"}, ExitCode: 0},
			{Version: Version{Tag: "nightly"}, ExitCode: 1},
			{Version: Version{Tag: "v0.10.4"}, Err: errors.New("x")},
		},
	}
	got := m.FailedVersions()
	want := []string{"nightly", "v0.10.4"}
	if len(got) != len(want) {
		t.Fatalf("FailedVersions len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i, tag := range want {
		if got[i] != tag {
			t.Errorf("FailedVersions[%d] = %q, want %q", i, got[i], tag)
		}
	}
}

func TestExecMatrix_FailedVersions_None(t *testing.T) {
	m := ExecMatrix{
		Results: []MatrixResult{{ExitCode: 0}, {ExitCode: 0}},
	}
	if got := m.FailedVersions(); got != nil {
		t.Errorf("FailedVersions = %v, want nil (no failures)", got)
	}
}

func TestExecMatrix_String(t *testing.T) {
	m := ExecMatrix{
		Results: []MatrixResult{
			{ExitCode: 0},
			{ExitCode: 1},
		},
		Duration: 1500 * time.Millisecond,
	}
	got := m.String()
	if !strings.Contains(got, "1/2 versions passed") {
		t.Errorf("String() = %q, want it to contain \"1/2 versions passed\"", got)
	}
	if !strings.Contains(got, "1.5s") {
		t.Errorf("String() = %q, want it to contain \"1.5s\"", got)
	}
}
