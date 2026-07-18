package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jedi-knights/neospec/internal/adapters/matrix"
	"github.com/jedi-knights/neospec/internal/adapters/neovim"
	"github.com/jedi-knights/neospec/internal/adapters/runner"
	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/config"
	"github.com/jedi-knights/neospec/internal/domain"
)

// execDeps holds injectable dependencies for runExec. Pass execDeps{} in
// production — zero-value (nil) fields cause the real adapters to be
// constructed. In tests, set individual fields to inject fakes without
// touching the network, filesystem, or subprocesses.
type execDeps struct {
	executor executor
	stdout   io.Writer
}

// executor is the abstraction runExec calls into. It matches the *matrix.Executor
// public shape so production wires the real adapter and tests wire fakes.
type executor interface {
	Run(ctx context.Context, argv []string, versions []domain.Version) (domain.ExecMatrix, error)
}

// execFlags holds values parsed from CLI flags for the exec command.
type execFlags struct {
	configPath string
	versions   string
	format     string
	cacheDir   string
	verbose    bool
}

// NewExecCmd builds the `neospec exec` command. Use `--` to separate neospec
// flags from the passthrough command, e.g.
//
//	neospec exec --versions=stable,nightly -- nvim --version
func NewExecCmd() *cobra.Command {
	flags := &execFlags{}

	cmd := &cobra.Command{
		Use:   "exec [flags] -- <cmd> [args...]",
		Short: "Run a command against multiple Neovim versions",
		Long: `exec runs an arbitrary command once per Neovim version and aggregates the outcome.

Use -- to separate neospec's own flags from the command being wrapped. Each
version's Neovim binary is placed first on PATH so 'nvim' inside your command
resolves to that specific build; the sandbox environment is applied so plugin
runs cannot read or mutate your real Neovim configuration.`,
		Example: `  neospec exec --versions=stable,nightly -- nvim --version
  neospec exec --versions=stable,nightly,v0.10.4 -- nvim --headless -c "PlenaryBustedDirectory tests/" -c "qa!"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExec(cmd.Context(), flags, args, execDeps{})
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.configPath, "config", "c", "neospec.toml", "path to config file")
	f.StringVar(&flags.versions, "versions", "", "comma-separated Neovim versions (e.g. stable,nightly,v0.10.4)")
	f.StringVar(&flags.format, "format", "console", "output format: console, json")
	f.StringVar(&flags.cacheDir, "cache-dir", "", "directory for cached Neovim binaries")
	f.BoolVarP(&flags.verbose, "verbose", "v", false, "verbose output")

	return cmd
}

func runExec(ctx context.Context, flags *execFlags, argv []string, deps execDeps) error {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flags.cacheDir != "" {
		cfg.CacheDir = flags.cacheDir
	}
	if flags.verbose {
		cfg.Verbose = true
	}

	versions, err := parseVersions(flags.versions)
	if err != nil {
		return err
	}

	platform, err := domain.CurrentPlatform()
	if err != nil {
		return fmt.Errorf("detecting platform: %w", err)
	}

	exec := deps.executor
	if exec == nil {
		exec = matrix.NewExecutor(
			neovim.NewProvider(cfg.CacheDir),
			sandbox.NewFactory(),
			realRunner{},
			platform,
		)
	}

	m, err := exec.Run(ctx, argv, versions)
	if err != nil {
		return fmt.Errorf("running matrix: %w", err)
	}

	out := deps.stdout
	if out == nil {
		out = os.Stdout
	}
	if err := writeMatrixReport(out, m, flags.format); err != nil {
		return err
	}

	if !m.Passed() {
		return fmt.Errorf("matrix failed: %d/%d version(s) failed", m.FailedCount(), len(m.Results))
	}
	return nil
}

// parseVersions splits a comma-separated version list and parses each entry.
// Empty entries are ignored so trailing commas do not error.
func parseVersions(spec string) ([]domain.Version, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("--versions is required (e.g. --versions=stable,nightly)")
	}
	parts := strings.Split(spec, ",")
	versions := make([]domain.Version, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := domain.ParseVersion(p)
		if err != nil {
			return nil, fmt.Errorf("--versions: %w", err)
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("--versions: no valid versions parsed from %q", spec)
	}
	return versions, nil
}

// writeMatrixReport emits the aggregated results in the requested format.
func writeMatrixReport(w io.Writer, m domain.ExecMatrix, format string) error {
	switch format {
	case "console":
		return writeConsoleReport(w, m)
	case "json":
		return writeJSONReport(w, m)
	default:
		return fmt.Errorf("unknown format %q: choose from console, json", format)
	}
}

// writeConsoleReport prints a per-version block followed by a summary line.
// It uses an errWriter so a single Write failure short-circuits the rest of
// the report rather than requiring every Fprintf call to be error-checked.
func writeConsoleReport(w io.Writer, m domain.ExecMatrix) error {
	ew := &errWriter{w: w}
	for _, r := range m.Results {
		writeResultBlock(ew, m.Command, r)
	}
	ew.printLn("----")
	ew.printLn(m.String())
	if failed := m.FailedVersions(); len(failed) > 0 {
		ew.printf("Failed versions: %s\n", strings.Join(failed, ", "))
	}
	return ew.err
}

// writeResultBlock renders a single version's stanza inside the console report.
func writeResultBlock(ew *errWriter, cmd []string, r domain.MatrixResult) {
	ew.printf("==> %s\n", r.Version.Tag)
	ew.printf("Running: %s\n", strings.Join(cmd, " "))
	if len(r.Stdout) > 0 {
		ew.print(string(r.Stdout))
		if !strings.HasSuffix(string(r.Stdout), "\n") {
			ew.printLn("")
		}
	}
	dur := r.Duration.Round(time.Millisecond)
	switch {
	case r.Err != nil:
		ew.printf("  x error: %s (%s)\n", r.Err, dur)
	case r.ExitCode == 0:
		ew.printf("  ok passed in %s\n", dur)
	default:
		ew.printf("  x failed in %s (exit code %d)\n", dur, r.ExitCode)
		if len(r.Stderr) > 0 {
			ew.printLn("stderr:")
			ew.print(string(r.Stderr))
			if !strings.HasSuffix(string(r.Stderr), "\n") {
				ew.printLn("")
			}
		}
	}
	ew.printLn("")
}

// errWriter is a small io.Writer wrapper that remembers the first write error
// and short-circuits all subsequent writes. It lets a report function issue
// many Fprintf-style calls without one errcheck-noqa per line.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}

func (e *errWriter) print(s string) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprint(e.w, s)
}

func (e *errWriter) printLn(s string) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintln(e.w, s)
}

// jsonMatrix is the wire shape for the JSON reporter, decoupled from the
// domain type so future domain changes do not break the JSON contract.
type jsonMatrix struct {
	Command  []string      `json:"command"`
	Versions []jsonVersion `json:"versions"`
	Summary  jsonSummary   `json:"summary"`
}

type jsonVersion struct {
	Version    string `json:"version"`
	Passed     bool   `json:"passed"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Error      string `json:"error,omitempty"`
}

type jsonSummary struct {
	Total      int   `json:"total"`
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	DurationMs int64 `json:"duration_ms"`
}

func writeJSONReport(w io.Writer, m domain.ExecMatrix) error {
	out := jsonMatrix{
		Command:  m.Command,
		Versions: make([]jsonVersion, 0, len(m.Results)),
		Summary: jsonSummary{
			Total:      len(m.Results),
			Passed:     m.PassedCount(),
			Failed:     m.FailedCount(),
			DurationMs: m.Duration.Milliseconds(),
		},
	}
	for _, r := range m.Results {
		v := jsonVersion{
			Version:    r.Version.Tag,
			Passed:     r.Passed(),
			ExitCode:   r.ExitCode,
			DurationMs: r.Duration.Milliseconds(),
			Stdout:     string(r.Stdout),
			Stderr:     string(r.Stderr),
		}
		if r.Err != nil {
			v.Error = r.Err.Error()
		}
		out.Versions = append(out.Versions, v)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// realRunner is a thin adapter that lets the exec command use runner's
// production CommandRunner without importing it via the runner package's
// unexported symbol. The runner package exposes NewWithDefaultSandbox but not
// a public CommandRunner constructor, so we wrap the same os/exec pattern here.
type realRunner struct{}

func (realRunner) Run(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	return runner.RunCommand(ctx, env, name, args...)
}
