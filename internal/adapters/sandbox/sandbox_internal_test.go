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
