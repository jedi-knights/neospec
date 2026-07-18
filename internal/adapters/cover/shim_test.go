package cover

import (
	"strings"
	"testing"
)

func TestBuildShim_PlenaryBusted(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:       RunnerPlenaryBusted,
		Dir:        "tests/",
		OutputFile: "/tmp/out.json",
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	s := string(shim)
	for _, want := range []string{
		"debug.sethook",                   // hook installed
		"_neospec_report",                 // reporter loaded
		"VimLeavePre",                     // exit-time autocmd
		`io.open("/tmp/out.json"`,         // output file wired
		"plenary.test_harness",            // plenary invoked
		`harness.test_directory("tests/"`, // dir escaped and inlined
		`vim.cmd("qa!")`,                  // explicit exit
	} {
		if !strings.Contains(s, want) {
			t.Errorf("shim missing %q\n---\n%s", want, s)
		}
	}
}

func TestBuildShim_MiniTest(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:       RunnerMiniTest,
		Dir:        "tests/**/*_test.lua",
		OutputFile: "/tmp/out.json",
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	s := string(shim)
	for _, want := range []string{
		"mini.test",
		`minitest.run`,
		`vim.fn.glob("tests/**/*_test.lua")`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("shim missing %q\n---\n%s", want, s)
		}
	}
}

func TestBuildShim_ExternalRejected(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerExternal, OutputFile: "/tmp/out.json"})
	if err == nil || !strings.Contains(err.Error(), "external mode does not use a shim") {
		t.Errorf("want external-rejection error, got: %v", err)
	}
}

func TestBuildShim_UnknownMode(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerMode("foo"), OutputFile: "/tmp/out.json"})
	if err == nil || !strings.Contains(err.Error(), "unknown runner mode") {
		t.Errorf("want unknown-mode error, got: %v", err)
	}
}

func TestBuildShim_MissingOutputFile(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerPlenaryBusted, Dir: "tests/"})
	if err == nil || !strings.Contains(err.Error(), "output file must not be empty") {
		t.Errorf("want missing-output error, got: %v", err)
	}
}

func TestBuildShim_MissingDir(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerPlenaryBusted, OutputFile: "/tmp/out.json"})
	if err == nil || !strings.Contains(err.Error(), "requires --dir") {
		t.Errorf("want missing-dir error, got: %v", err)
	}
}

func TestBuildShim_NULRejected(t *testing.T) {
	cases := []struct {
		name string
		opts ShimOpts
	}{
		{"NUL in output", ShimOpts{Mode: RunnerPlenaryBusted, Dir: "tests/", OutputFile: "/tmp/\x00out.json"}},
		{"NUL in dir", ShimOpts{Mode: RunnerPlenaryBusted, Dir: "tests/\x00foo", OutputFile: "/tmp/out.json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildShim(tc.opts)
			if err == nil || !strings.Contains(err.Error(), "NUL") {
				t.Errorf("want NUL-rejection error, got: %v", err)
			}
		})
	}
}

func TestLuaEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`plain`, `plain`},
		{`with"quote`, `with\"quote`},
		{`with\back`, `with\\back`},
		{"with\nnl", `with\nnl`},
		{"with\ttab", `with\ttab`},
		{`combo"\`, `combo\"\\`},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := luaEscape(tc.in); got != tc.want {
				t.Errorf("luaEscape(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
