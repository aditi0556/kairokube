package checkpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRestoreBuildsStartCheckpointCommand(t *testing.T) {
	dir := t.TempDir()
	checkpoint := filepath.Join(dir, "checkpoint-1")
	if err := os.Mkdir(checkpoint, 0700); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs []string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return []byte("abc123\n"), nil
	}

	id, err := NewDockerRestorer(runner).RestoreArchive(context.Background(), checkpoint, "target-container")
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if id != "abc123" {
		t.Fatalf("id = %q, want abc123", id)
	}
	if gotName != "docker" {
		t.Fatalf("command = %q, want docker", gotName)
	}
	want := []string{"start", "--checkpoint", "checkpoint-1", "--checkpoint-dir", dir, "target-container"}
	if strings.Join(gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", gotArgs, want)
	}
}

func TestDockerRestoreAcceptsCheckpointFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.tar")
	if err := os.WriteFile(path, []byte("checkpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	var gotArgs []string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return nil, nil
	}
	if id, err := NewDockerRestorer(runner).RestoreArchive(context.Background(), path, "target"); err != nil || id != "target" {
		t.Fatalf("got %q, %v; want target, nil", id, err)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "--checkpoint checkpoint.tar --checkpoint-dir "+dir) {
		t.Fatalf("args = %v", gotArgs)
	}
}

func TestDockerRestoreRejectsInvalidInputsWithoutRunning(t *testing.T) {
	called := false
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		called = true
		return []byte("id"), nil
	}
	r := NewDockerRestorer(runner)
	for _, tc := range [][2]string{{"", "target"}, {"/missing/checkpoint", "target"}, {t.TempDir(), ""}} {
		if _, err := r.RestoreArchive(context.Background(), tc[0], tc[1]); err == nil {
			t.Errorf("RestoreArchive(%q, %q) returned nil error", tc[0], tc[1])
		}
	}
	if called {
		t.Fatal("runner must not be called for invalid input")
	}
}

func TestDockerRestoreSurfacesCommandFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("exit status 125")
	}
	if _, err := NewDockerRestorer(runner).RestoreArchive(context.Background(), path, "target"); err == nil {
		t.Fatal("expected command failure")
	}
}

func TestDockerRestoreNilRestorerFailsClearly(t *testing.T) {
	var r *DockerRestorer
	if _, err := r.RestoreArchive(context.Background(), "checkpoint", "target"); !errors.Is(err, ErrDockerRestoreUnavailable) {
		t.Fatalf("expected ErrDockerRestoreUnavailable, got %v", err)
	}
}
