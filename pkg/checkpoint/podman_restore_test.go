package checkpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArchive(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checkpoint.tar.gz")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return path
}

func TestPodmanRestoreReturnsContainerID(t *testing.T) {
	archive := writeArchive(t, "archive-bytes")
	var gotName string
	var gotArgs []string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = args
		return []byte("abc123def456\n"), nil
	}

	id, err := NewPodmanRestorer(runner).Restore(context.Background(), archive, "restored-counter")
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if id != "abc123def456" {
		t.Fatalf("id = %q, want abc123def456", id)
	}
	if gotName != "podman" {
		t.Fatalf("command = %q, want podman", gotName)
	}
	want := []string{"container", "restore", "--import", archive, "--name", "restored-counter"}
	if strings.Join(gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", gotArgs, want)
	}
}

func TestPodmanRestoreRejectsInvalidInputsWithoutRunning(t *testing.T) {
	called := false
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		called = true
		return []byte("x"), nil
	}
	r := NewPodmanRestorer(runner)
	empty := writeArchive(t, "")

	cases := map[string]struct{ archive, name string }{
		"missing archive path": {"", "c"},
		"missing name":         {writeArchive(t, "x"), " "},
		"nonexistent archive":  {filepath.Join(t.TempDir(), "nope.tar"), "c"},
		"empty archive":        {empty, "c"},
	}
	for label, tc := range cases {
		if _, err := r.Restore(context.Background(), tc.archive, tc.name); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
	if called {
		t.Fatal("runner must not be called for invalid inputs")
	}
}

func TestPodmanRestoreSurfacesCommandFailure(t *testing.T) {
	archive := writeArchive(t, "archive-bytes")
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("exit status 125")
	}
	if _, err := NewPodmanRestorer(runner).Restore(context.Background(), archive, "c"); err == nil {
		t.Fatal("expected command failure to be returned")
	}
}

func TestPodmanRestoreRequiresNonEmptyID(t *testing.T) {
	archive := writeArchive(t, "archive-bytes")
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("  \n"), nil
	}
	if _, err := NewPodmanRestorer(runner).Restore(context.Background(), archive, "c"); err == nil {
		t.Fatal("expected error when podman returns no container ID")
	}
}

func TestPodmanRestoreWithoutRunnerFailsClearly(t *testing.T) {
	var r *PodmanRestorer
	if _, err := r.Restore(context.Background(), "x", "c"); !errors.Is(err, ErrRestoreUnavailable) {
		t.Fatalf("expected ErrRestoreUnavailable, got %v", err)
	}
}
