// White-box tests for the fsOps injection point. These test the MkdirTemp and
// MkdirAll error paths inside Factory.Create, which are unreachable with the
// real OS implementation but are meaningful failure scenarios in containers
// and constrained environments.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// errorMkdirTemp is an fsOps that always fails on MkdirTemp.
type errorMkdirTemp struct{}

func (errorMkdirTemp) MkdirTemp(_, _ string) (string, error) {
	return "", fmt.Errorf("simulated MkdirTemp failure")
}
func (errorMkdirTemp) MkdirAll(_ string, _ os.FileMode) error { return nil }
func (errorMkdirTemp) RemoveAll(_ string) error               { return nil }

// errorMkdirAll is an fsOps that succeeds on MkdirTemp but fails on MkdirAll.
type errorMkdirAll struct {
	dir string
}

func (e *errorMkdirAll) MkdirTemp(_, _ string) (string, error) {
	return e.dir, nil
}
func (*errorMkdirAll) MkdirAll(_ string, _ os.FileMode) error {
	return fmt.Errorf("simulated MkdirAll failure")
}
func (*errorMkdirAll) RemoveAll(_ string) error { return nil }

// TestFactory_Create_MkdirTempError verifies that a MkdirTemp failure is
// propagated as an error from Factory.Create.
func TestFactory_Create_MkdirTempError(t *testing.T) {
	f := &Factory{fs: errorMkdirTemp{}}
	_, err := f.Create(context.Background())
	if err == nil {
		t.Fatal("Create() expected error when MkdirTemp fails, got nil")
	}
}

// TestFactory_Create_MkdirAllError verifies that a MkdirAll failure is
// propagated as an error from Factory.Create and the temp directory is cleaned up.
func TestFactory_Create_MkdirAllError(t *testing.T) {
	tmpDir := t.TempDir()
	f := &Factory{fs: &errorMkdirAll{dir: tmpDir}}
	_, err := f.Create(context.Background())
	if err == nil {
		t.Fatal("Create() expected error when MkdirAll fails, got nil")
	}
}

// errorMkdirAllAndRemoveAll is an fsOps that fails on both MkdirAll and RemoveAll,
// exercising the errors.Join branch in Factory.Create.
type errorMkdirAllAndRemoveAll struct {
	dir string
}

func (e *errorMkdirAllAndRemoveAll) MkdirTemp(_, _ string) (string, error) {
	return e.dir, nil
}
func (*errorMkdirAllAndRemoveAll) MkdirAll(_ string, _ os.FileMode) error {
	return fmt.Errorf("simulated MkdirAll failure")
}
func (*errorMkdirAllAndRemoveAll) RemoveAll(_ string) error {
	return fmt.Errorf("simulated RemoveAll failure")
}

// TestFactory_Create_MkdirAllAndRemoveAllError covers the errors.Join path
// when MkdirAll fails AND the subsequent RemoveAll cleanup also fails.
func TestFactory_Create_MkdirAllAndRemoveAllError(t *testing.T) {
	tmpDir := t.TempDir()
	f := &Factory{fs: &errorMkdirAllAndRemoveAll{dir: tmpDir}}
	_, err := f.Create(context.Background())
	if err == nil {
		t.Fatal("Create() expected joined error when both MkdirAll and RemoveAll fail, got nil")
	}
}

// flakyRemoveAll is an fsOps whose RemoveAll fails the first failTimes calls
// (with an ENOTEMPTY-style error) and then succeeds, modeling a still-running
// grandchild process writing into the sandbox during the first teardown attempt.
type flakyRemoveAll struct {
	failTimes int
	calls     int
}

func (*flakyRemoveAll) MkdirTemp(_, _ string) (string, error)  { return "", nil }
func (*flakyRemoveAll) MkdirAll(_ string, _ os.FileMode) error { return nil }
func (f *flakyRemoveAll) RemoveAll(_ string) error {
	f.calls++
	if f.calls <= f.failTimes {
		return fmt.Errorf("unlinkat /tmp/x/lazy/lazy.nvim: directory not empty")
	}
	return nil
}

// TestSandbox_Close_RetriesTransientRemoveAll verifies that Close() retries a
// transiently-failing RemoveAll and returns nil once it succeeds within the
// retry budget. This is the residual-race guard: even after the runner kills
// the nvim process group, in-flight writes can briefly make RemoveAll fail.
func TestSandbox_Close_RetriesTransientRemoveAll(t *testing.T) {
	fs := &flakyRemoveAll{failTimes: 3}
	s := &xdgSandbox{
		root:     "/tmp/neospec-sandbox-test",
		fs:       fs,
		backoffs: []time.Duration{0, 0, 0, 0, 0}, // 5 retries, no real sleeping
		sleep:    func(time.Duration) {},
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() should succeed after transient failures, got: %v", err)
	}
	if fs.calls != 4 { // 3 failures + 1 success
		t.Fatalf("expected 4 RemoveAll attempts (3 fail + 1 success), got %d", fs.calls)
	}
}

// TestSandbox_Close_SurfacesPersistentRemoveAll verifies that a RemoveAll that
// fails on every attempt still surfaces the error (the retry is bounded — it
// must not loop forever or swallow a genuine cleanup failure).
func TestSandbox_Close_SurfacesPersistentRemoveAll(t *testing.T) {
	fs := &flakyRemoveAll{failTimes: 1000} // never succeeds within budget
	backoffs := []time.Duration{0, 0, 0}
	s := &xdgSandbox{
		root:     "/tmp/neospec-sandbox-test",
		fs:       fs,
		backoffs: backoffs,
		sleep:    func(time.Duration) {},
	}

	err := s.Close()
	if err == nil {
		t.Fatal("Close() expected error when RemoveAll fails on every attempt, got nil")
	}
	if want := len(backoffs) + 1; fs.calls != want { // initial attempt + one per backoff
		t.Fatalf("expected %d bounded RemoveAll attempts, got %d", want, fs.calls)
	}
}
