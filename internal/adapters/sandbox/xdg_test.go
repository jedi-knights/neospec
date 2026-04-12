package sandbox_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
)

func TestFactory_Create(t *testing.T) {
	f := sandbox.NewFactory()
	sb, err := f.Create(context.Background())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	t.Cleanup(func() { sb.Close() })

	dir := sb.Dir()
	if dir == "" {
		t.Fatal("Dir() returned empty string")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("sandbox root dir does not exist: %v", err)
	}
}

func TestSandbox_Env(t *testing.T) {
	f := sandbox.NewFactory()
	sb, err := f.Create(context.Background())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	t.Cleanup(func() { sb.Close() })

	env := sb.Env()
	if len(env) == 0 {
		t.Fatal("Env() returned empty slice")
	}

	required := []string{
		"XDG_DATA_HOME=",
		"XDG_CONFIG_HOME=",
		"XDG_STATE_HOME=",
		"XDG_CACHE_HOME=",
		"XDG_RUNTIME_DIR=",
		"NVIM_APPNAME=neospec-isolated",
		"HOME=",
	}
	for _, prefix := range required {
		found := false
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Env() missing entry with prefix %q\ngot: %v", prefix, env)
		}
	}
}

func TestSandbox_EnvDirsExist(t *testing.T) {
	f := sandbox.NewFactory()
	sb, err := f.Create(context.Background())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	t.Cleanup(func() { sb.Close() })

	dir := sb.Dir()
	for _, sub := range []string{"data", "config", "state", "cache", "runtime"} {
		path := filepath.Join(dir, sub)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected subdir %q to exist: %v", sub, err)
		}
	}
}

func TestSandbox_Close(t *testing.T) {
	f := sandbox.NewFactory()
	sb, err := f.Create(context.Background())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	dir := sb.Dir()
	if err := sb.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected sandbox dir to be removed after Close(), stat error: %v", err)
	}
}
