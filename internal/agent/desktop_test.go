package agent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeDesktopExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCodexDesktopAppExplicitExecutableSkipsSearch(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "ChatGPT")
	writeDesktopExecutable(t, explicit)

	got, err := ResolveCodexDesktopApp(DesktopResolveOptions{
		GOOS:     "linux",
		Home:     t.TempDir(),
		Explicit: explicit,
		SearchRoots: []string{
			filepath.Join(t.TempDir(), "must-not-scan"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != explicit {
		t.Fatalf("executable = %q, want %q", got, explicit)
	}
}

func TestResolveCodexDesktopAppExplicitDirectoryNormalizesExecutable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Codex")
	exe := filepath.Join(dir, "codex")
	writeDesktopExecutable(t, exe)

	got, err := ResolveCodexDesktopApp(DesktopResolveOptions{
		GOOS:     "linux",
		Home:     t.TempDir(),
		Explicit: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("executable = %q, want %q", got, exe)
	}
}

func TestResolveCodexDesktopAppSearchRootsAndAppSubdirectory(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "chatgpt", "app")
	exe := filepath.Join(app, "ChatGPT")
	writeDesktopExecutable(t, exe)

	got, err := ResolveCodexDesktopApp(DesktopResolveOptions{
		GOOS:        "linux",
		Home:        t.TempDir(),
		SearchRoots: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("executable = %q, want %q", got, exe)
	}
}

func TestResolveCodexDesktopAppRejectsUnrecognizedNames(t *testing.T) {
	root := t.TempDir()
	writeDesktopExecutable(t, filepath.Join(root, "unknown", "helper"))

	if _, err := ResolveCodexDesktopApp(DesktopResolveOptions{GOOS: "linux", Home: t.TempDir(), SearchRoots: []string{root}}); err == nil {
		t.Fatal("unrecognized app should fail")
	}

	explicitDir := filepath.Join(root, "unknown")
	if _, err := ResolveCodexDesktopApp(DesktopResolveOptions{GOOS: "linux", Home: t.TempDir(), Explicit: explicitDir}); err == nil {
		t.Fatal("explicit unrecognized app should fail")
	}
}

func TestResolveCodexDesktopAppMissingHasActionableError(t *testing.T) {
	home := t.TempDir()
	_, err := ResolveCodexDesktopApp(DesktopResolveOptions{
		GOOS:              "linux",
		Home:              home,
		SearchRoots:       nil,
		SearchRootScanner: func(string) (string, error) { return "", nil },
	})
	if err == nil {
		t.Fatal("missing app should fail")
	}
	for _, want := range []string{"/usr/lib", "/opt", filepath.Join(home, "Applications"), filepath.Join(home, ".local", "share"), "AGW_CODEX_APP", "ChatGPT", "codex"} {
		if !containsString(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestResolveCodexDesktopAppAutomaticDiscoveryOnlyOnLinux(t *testing.T) {
	called := false
	opts := DesktopResolveOptions{
		GOOS: "darwin",
		Home: t.TempDir(),
		SearchRootScanner: func(root string) (string, error) {
			called = true
			return "", nil
		},
	}
	if _, err := ResolveCodexDesktopApp(opts); err == nil || !containsString(err.Error(), "仅支持 Linux") {
		t.Fatalf("unsupported error = %v", err)
	}
	if called {
		t.Fatal("non-Linux discovery must not scan")
	}
}

func TestResolveCodexDesktopAppRejectsMissingOrUnexecutablePath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing", "ChatGPT")
	if _, err := ResolveCodexDesktopApp(DesktopResolveOptions{GOOS: "linux", Home: dir, Explicit: missing}); err == nil {
		t.Fatal("missing executable should fail")
	}

	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if _, err := ResolveCodexDesktopApp(DesktopResolveOptions{GOOS: "linux", Home: dir, Explicit: plain}); err == nil {
			t.Fatal("unexecutable path should fail on linux")
		}
	}

	isExec := func(path string) bool { return path == plain }
	if _, err := ResolveCodexDesktopApp(DesktopResolveOptions{GOOS: "linux", Home: dir, Explicit: plain, IsExecutable: isExec}); err != nil {
		t.Fatalf("injected executable check should pass: %v", err)
	}
}

func TestResolveCodexDesktopAppDefaultsAreBounded(t *testing.T) {
	home := t.TempDir()
	var roots []string
	_, _ = ResolveCodexDesktopApp(DesktopResolveOptions{
		GOOS: "linux",
		Home: home,
		SearchRootScanner: func(root string) (string, error) {
			roots = append(roots, root)
			return "", os.ErrNotExist
		},
	})
	want := DefaultLinuxDesktopRoots(home)
	if len(roots) != len(want) {
		t.Fatalf("roots = %v, want %v", roots, want)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("roots = %v, want %v", roots, want)
		}
	}
}

func TestResolveCodexDesktopAppScannerErrorStops(t *testing.T) {
	boom := errors.New("boom")
	_, err := ResolveCodexDesktopApp(DesktopResolveOptions{
		GOOS: "linux",
		Home: t.TempDir(),
		SearchRootScanner: func(root string) (string, error) {
			return "", boom
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringIndex(s, substr) >= 0)
}

func stringIndex(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
