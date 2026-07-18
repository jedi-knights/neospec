package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestParseVersions_Valid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single stable", "stable", []string{"stable"}},
		{"stable and nightly", "stable,nightly", []string{"stable", "nightly"}},
		{"with semver", "stable,v0.10.4,nightly", []string{"stable", "v0.10.4", "nightly"}},
		{"trailing comma", "stable,nightly,", []string{"stable", "nightly"}},
		{"whitespace tolerated", "stable, nightly ,  v0.10.4  ", []string{"stable", "nightly", "v0.10.4"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVersions(tc.in)
			if err != nil {
				t.Fatalf("parseVersions(%q) error: %v", tc.in, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d versions, want %d (%v)", len(got), len(tc.want), got)
			}
			for i, tag := range tc.want {
				if got[i].Tag != tag {
					t.Errorf("[%d] got %q, want %q", i, got[i].Tag, tag)
				}
			}
		})
	}
}

func TestParseVersions_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantSub string
	}{
		{"empty", "", "is required"},
		{"whitespace only", "   ", "is required"},
		{"only comma", ",", "no valid versions"},
		{"garbage semver", "stable,bogus", "invalid neovim version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseVersions(tc.in)
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestWriteConsoleReport_AllPassed(t *testing.T) {
	m := domain.ExecMatrix{
		Command: []string{"nvim", "--version"},
		Results: []domain.MatrixResult{
			{Version: domain.Version{Tag: "stable"}, ExitCode: 0, Stdout: []byte("NVIM v0.10.4\n"), Duration: 300 * time.Millisecond},
			{Version: domain.Version{Tag: "nightly"}, ExitCode: 0, Stdout: []byte("NVIM v0.11.0-dev\n"), Duration: 500 * time.Millisecond},
		},
		Duration: 800 * time.Millisecond,
	}
	var buf bytes.Buffer
	if err := writeConsoleReport(&buf, m); err != nil {
		t.Fatalf("writeConsoleReport: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"==> stable", "==> nightly",
		"NVIM v0.10.4", "NVIM v0.11.0-dev",
		"passed", "2/2 versions passed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "Failed versions:") {
		t.Errorf("all-passed run should not print Failed versions line")
	}
}

func TestWriteConsoleReport_MixedOutcome(t *testing.T) {
	m := domain.ExecMatrix{
		Command: []string{"nvim", "-c", "qa!"},
		Results: []domain.MatrixResult{
			{Version: domain.Version{Tag: "stable"}, ExitCode: 0, Duration: 100 * time.Millisecond},
			{Version: domain.Version{Tag: "nightly"}, ExitCode: 1, Stderr: []byte("E5108\n"), Duration: 200 * time.Millisecond},
			{Version: domain.Version{Tag: "v0.10.4"}, Err: errors.New("download failed"), Duration: 5 * time.Second},
		},
		Duration: 5300 * time.Millisecond,
	}
	var buf bytes.Buffer
	if err := writeConsoleReport(&buf, m); err != nil {
		t.Fatalf("writeConsoleReport: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"passed", "failed", "error: download failed",
		"stderr:", "E5108",
		"1/3 versions passed", "Failed versions: nightly, v0.10.4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestWriteJSONReport_Shape(t *testing.T) {
	m := domain.ExecMatrix{
		Command: []string{"nvim", "--version"},
		Results: []domain.MatrixResult{
			{Version: domain.Version{Tag: "stable"}, ExitCode: 0, Stdout: []byte("ok\n"), Duration: 300 * time.Millisecond},
			{Version: domain.Version{Tag: "nightly"}, ExitCode: 1, Stderr: []byte("bad\n"), Duration: 500 * time.Millisecond, Err: errors.New("boom")},
		},
		Duration: 800 * time.Millisecond,
	}
	var buf bytes.Buffer
	if err := writeJSONReport(&buf, m); err != nil {
		t.Fatalf("writeJSONReport: %v", err)
	}
	var got jsonMatrix
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("emitted invalid JSON: %v\n---\n%s", err, buf.String())
	}
	if got.Summary.Total != 2 || got.Summary.Passed != 1 || got.Summary.Failed != 1 {
		t.Errorf("summary = %+v, want total=2 passed=1 failed=1", got.Summary)
	}
	if got.Summary.DurationMs != 800 {
		t.Errorf("summary duration = %d, want 800ms", got.Summary.DurationMs)
	}
	if len(got.Versions) != 2 {
		t.Fatalf("versions len = %d, want 2", len(got.Versions))
	}
	if got.Versions[1].Error != "boom" {
		t.Errorf("nightly error = %q, want %q", got.Versions[1].Error, "boom")
	}
	if got.Versions[0].Error != "" {
		t.Errorf("stable error = %q, want empty", got.Versions[0].Error)
	}
}

func TestWriteMatrixReport_UnknownFormat(t *testing.T) {
	err := writeMatrixReport(&bytes.Buffer{}, domain.ExecMatrix{}, "yaml")
	if err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("want unknown-format error, got: %v", err)
	}
}

// fakeExecutor is the deps injection for runExec tests.
type fakeExecutor struct {
	matrix domain.ExecMatrix
	err    error
	calls  int
}

func (f *fakeExecutor) Run(_ context.Context, _ []string, _ []domain.Version) (domain.ExecMatrix, error) {
	f.calls++
	return f.matrix, f.err
}

func TestRunExec_HappyPath(t *testing.T) {
	fake := &fakeExecutor{matrix: domain.ExecMatrix{
		Command: []string{"nvim", "--version"},
		Results: []domain.MatrixResult{{Version: domain.Version{Tag: "stable"}, ExitCode: 0}},
	}}
	var out bytes.Buffer
	err := runExec(context.Background(),
		&execFlags{configPath: "nonexistent.toml", versions: "stable", format: "console"},
		[]string{"nvim", "--version"},
		execDeps{executor: fake, stdout: &out},
	)
	if err != nil {
		t.Fatalf("runExec: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("executor called %d times, want 1", fake.calls)
	}
	if !strings.Contains(out.String(), "stable") {
		t.Errorf("output missing stable version: %q", out.String())
	}
}

func TestRunExec_MatrixFailure(t *testing.T) {
	fake := &fakeExecutor{matrix: domain.ExecMatrix{
		Command: []string{"nvim"},
		Results: []domain.MatrixResult{{Version: domain.Version{Tag: "stable"}, ExitCode: 1}},
	}}
	err := runExec(context.Background(),
		&execFlags{configPath: "nonexistent.toml", versions: "stable", format: "console"},
		[]string{"nvim"},
		execDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "matrix failed") {
		t.Errorf("want matrix-failed error, got: %v", err)
	}
}

func TestRunExec_ParseVersionsError(t *testing.T) {
	err := runExec(context.Background(),
		&execFlags{configPath: "nonexistent.toml", versions: ""},
		[]string{"nvim"},
		execDeps{executor: &fakeExecutor{}, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "--versions is required") {
		t.Errorf("want required-flag error, got: %v", err)
	}
}

func TestRunExec_ExecutorError(t *testing.T) {
	fake := &fakeExecutor{err: errors.New("catastrophic")}
	err := runExec(context.Background(),
		&execFlags{configPath: "nonexistent.toml", versions: "stable", format: "console"},
		[]string{"nvim"},
		execDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "catastrophic") {
		t.Errorf("want executor error propagated, got: %v", err)
	}
}

// failingWriter always returns a write error; used to exercise errWriter's
// short-circuit path so a Fprintf failure aborts the report cleanly instead
// of ballooning into a nil deref or a partial output.
type failingWriter struct{ err error }

func (f failingWriter) Write(_ []byte) (int, error) { return 0, f.err }

func TestWriteConsoleReport_WriteFailure(t *testing.T) {
	m := domain.ExecMatrix{
		Command: []string{"nvim"},
		Results: []domain.MatrixResult{{Version: domain.Version{Tag: "stable"}, ExitCode: 0}},
	}
	err := writeConsoleReport(failingWriter{err: errors.New("pipe broken")}, m)
	if err == nil || !strings.Contains(err.Error(), "pipe broken") {
		t.Errorf("want write-failure error surfaced, got: %v", err)
	}
}

func TestNewExecCmd_FlagDefaults(t *testing.T) {
	cmd := NewExecCmd()
	if cmd.Use == "" {
		t.Errorf("Use is empty")
	}
	if cmd.Args == nil {
		t.Errorf("Args validator is nil — command would accept zero-arg invocations")
	}
	if err := cmd.Flags().Parse(nil); err != nil {
		t.Fatalf("flag parse: %v", err)
	}
	if got, _ := cmd.Flags().GetString("format"); got != "console" {
		t.Errorf("format default = %q, want \"console\"", got)
	}
	if got, _ := cmd.Flags().GetString("config"); got != "neospec.toml" {
		t.Errorf("config default = %q, want \"neospec.toml\"", got)
	}
}
