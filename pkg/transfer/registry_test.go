package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryTransferPushesCheckpointArtifact(t *testing.T) {
	checkpoint := filepath.Join(t.TempDir(), "checkpoint.tar")
	if err := os.WriteFile(checkpoint, []byte("checkpoint-data"), 0600); err != nil {
		t.Fatal(err)
	}
	var commands [][]string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	}
	p := NewRegistryTransferProvider("registry.example/kairokube/checkpoints", runner)
	result, err := p.Transfer(context.Background(), checkpoint, "worker-2", "", "")
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}
	if !strings.HasPrefix(result.DestinationPath, "oci://registry.example/kairokube/checkpoints:") {
		t.Fatalf("destination = %q", result.DestinationPath)
	}
	if len(commands) != 1 || commands[0][0] != "oras" || commands[0][1] != "push" {
		t.Fatalf("commands = %v", commands)
	}
	if !strings.Contains(strings.Join(commands[0], " "), "--artifact-type "+checkpointArtifactType) {
		t.Fatalf("push command = %v", commands[0])
	}
}

func TestRegistryTransferValidatesExpectedChecksum(t *testing.T) {
	checkpoint := filepath.Join(t.TempDir(), "checkpoint.tar")
	if err := os.WriteFile(checkpoint, []byte("checkpoint-data"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	if _, err := NewRegistryTransferProvider("registry.example/repo", runner).Transfer(context.Background(), checkpoint, "worker", "", "bad"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if called {
		t.Fatal("registry push must not run after checksum mismatch")
	}
}

func TestRegistryTransferRequiresRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.tar")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistryTransferProvider("", func(context.Context, string, ...string) ([]byte, error) { return nil, nil }).Transfer(context.Background(), path, "worker", "", ""); err == nil {
		t.Fatal("expected missing repository error")
	}
}
