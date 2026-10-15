package checkpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCtrRestorerBuildsLiveRestoreCommand(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return []byte("restored-id\n"), nil
	}
	id, err := NewCtrRestorer(run).RestoreArchive(context.Background(), "/tmp/a.tar", "counter-1")
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if id != "restored-id" {
		t.Fatalf("id = %q", id)
	}
	if gotName != "ctr" {
		t.Fatalf("command = %q, want ctr", gotName)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-n k8s.io", "containers restore --live", "counter-1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestCtrRestorerFallsBackToRequestedID(t *testing.T) {
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) { return nil, nil }
	id, err := NewCtrRestorer(run).RestoreArchive(context.Background(), "/tmp/a.tar", "counter-2")
	if err != nil || id != "counter-2" {
		t.Fatalf("got %q, %v; want counter-2, nil", id, err)
	}
}

func TestCtrRestorerSurfacesFailure(t *testing.T) {
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	}
	if _, err := NewCtrRestorer(run).RestoreArchive(context.Background(), "/tmp/a.tar", "c"); err == nil {
		t.Fatal("expected command failure")
	}
}

func TestCtrRestorerRejectsMissingInputsAndNilRunner(t *testing.T) {
	called := false
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	if _, err := NewCtrRestorer(run).RestoreArchive(context.Background(), " ", "c"); err == nil {
		t.Error("empty archive path must fail")
	}
	if _, err := NewCtrRestorer(run).RestoreArchive(context.Background(), "/tmp/a.tar", ""); err == nil {
		t.Error("empty container ID must fail")
	}
	if called {
		t.Fatal("runner must not run for invalid input")
	}
	var nilRestorer *CtrRestorer
	if _, err := nilRestorer.RestoreArchive(context.Background(), "/tmp/a.tar", "c"); !errors.Is(err, ErrNodeRestoreUnavailable) {
		t.Fatalf("nil restorer: got %v, want ErrNodeRestoreUnavailable", err)
	}
}

func TestCheckPreflight(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar")
	criu := filepath.Join(dir, "criu")
	if err := os.WriteFile(archive, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(criu, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := CheckPreflight(Preflight{ArchivePath: archive, CRIUPath: criu}); err != nil {
		t.Fatalf("valid preflight failed: %v", err)
	}
	if err := CheckPreflight(Preflight{ArchivePath: filepath.Join(dir, "missing.tar"), CRIUPath: criu}); err == nil {
		t.Error("missing archive must fail")
	}
	empty := filepath.Join(dir, "empty.tar")
	_ = os.WriteFile(empty, nil, 0600)
	if err := CheckPreflight(Preflight{ArchivePath: empty, CRIUPath: criu}); err == nil {
		t.Error("empty archive must fail")
	}
	if err := CheckPreflight(Preflight{ArchivePath: archive, CRIUPath: filepath.Join(dir, "nocriu")}); err == nil {
		t.Error("missing CRIU must fail")
	}
}
