package transfer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const checkpointArtifactType = "application/vnd.kairokube.checkpoint.v1"

// CommandRunner executes a CLI command and returns combined output.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type commandRunnerWithInput func(ctx context.Context, name string, input string, args ...string) ([]byte, error)

// DefaultCommandRunner executes commands through the local operating system.
var DefaultCommandRunner CommandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

var defaultCommandRunnerWithInput commandRunnerWithInput = func(ctx context.Context, name, input string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.CombinedOutput()
}

// RegistryTransferProvider packages a checkpoint as a compressed OCI artifact
// and pushes it with ORAS. The returned oci:// reference is consumed by the
// Docker restorer on the target node.
type RegistryTransferProvider struct {
	Repository string
	Username   string
	Password   string
	run        CommandRunner
	runInput   commandRunnerWithInput
}

// NewRegistryTransferProvider creates a registry-backed checkpoint transfer.
// The ORAS CLI must already be authenticated to the registry.
func NewRegistryTransferProvider(repository string, run CommandRunner) *RegistryTransferProvider {
	usingDefaultRunner := run == nil
	if run == nil {
		run = DefaultCommandRunner
	}
	inputRunner := defaultCommandRunnerWithInput
	if !usingDefaultRunner {
		inputRunner = nil
	}
	return &RegistryTransferProvider{Repository: strings.TrimRight(strings.TrimSpace(repository), "/"), run: run, runInput: inputRunner}
}

// WithCredentials configures ORAS registry credentials. Existing ORAS auth
// configuration is used when credentials are empty.
func (p *RegistryTransferProvider) WithCredentials(username, password string) *RegistryTransferProvider {
	p.Username, p.Password = username, password
	return p
}

// Transfer archives sourcePath, pushes it to the configured OCI repository,
// and returns an oci:// reference for the target-side restore.
func (p *RegistryTransferProvider) Transfer(ctx context.Context, sourcePath, targetNode, _ string, expectedChecksum string) (*TransferResult, error) {
	start := time.Now()
	result := &TransferResult{SourcePath: sourcePath, TargetNode: targetNode, StartTime: start}
	finish := func(err error) (*TransferResult, error) {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(start)
		result.Success = err == nil
		if err != nil {
			result.Error = err.Error()
		}
		return result, err
	}
	if p == nil || p.run == nil {
		return finish(fmt.Errorf("registry transfer provider is not configured"))
	}
	if p.Repository == "" {
		return finish(fmt.Errorf("checkpoint registry repository is required"))
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return finish(fmt.Errorf("source checkpoint does not exist: %w", err))
	}
	archive, err := os.CreateTemp("", "kairokube-checkpoint-*.tar.gz")
	if err != nil {
		return finish(fmt.Errorf("create checkpoint staging archive: %w", err))
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	if err := writeCheckpointArchive(archive, sourcePath, info.IsDir()); err != nil {
		archive.Close()
		return finish(err)
	}
	if err := archive.Close(); err != nil {
		return finish(fmt.Errorf("close checkpoint staging archive: %w", err))
	}

	sourceChecksum, _, err := fileDigest(sourcePath)
	if err != nil {
		if !info.IsDir() {
			return finish(err)
		}
		sourceChecksum = ""
	}
	archiveChecksum, archiveSize, err := fileDigest(archivePath)
	if err != nil {
		return finish(err)
	}
	if expectedChecksum != "" && sourceChecksum != expectedChecksum {
		return finish(fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, sourceChecksum))
	}
	if sourceChecksum == "" {
		sourceChecksum = archiveChecksum
	}
	ref := fmt.Sprintf("%s:%s-%s", p.Repository, safeTag(filepath.Base(sourcePath)), archiveChecksum[:12])
	if err := p.login(ctx, ref); err != nil {
		return finish(err)
	}
	if _, err := p.run(ctx, "oras", "push", ref, archivePath+":"+checkpointArtifactType, "--artifact-type", checkpointArtifactType); err != nil {
		return finish(fmt.Errorf("oras push failed: %w", err))
	}
	result.DestinationPath = "oci://" + ref
	result.SizeBytes = archiveSize
	result.Checksum = sourceChecksum
	return finish(nil)
}

func (p *RegistryTransferProvider) login(ctx context.Context, ref string) error {
	if p.Username == "" && p.Password == "" {
		return nil
	}
	host := strings.SplitN(ref, "/", 2)[0]
	if p.Username == "" || p.Password == "" {
		return fmt.Errorf("registry username and password must be provided together")
	}
	var err error
	if p.runInput != nil {
		_, err = p.runInput(ctx, "oras", p.Password+"\n", "login", "-u", p.Username, "--password-stdin", host)
	} else {
		return fmt.Errorf("registry credentials require the default command runner")
	}
	if err != nil {
		return fmt.Errorf("oras login failed: %w", err)
	}
	return nil
}

func writeCheckpointArchive(dst io.Writer, sourcePath string, sourceIsDir bool) error {
	gz := gzip.NewWriter(dst)
	tw := tar.NewWriter(gz)
	root := sourcePath
	if sourceIsDir {
		root = filepath.Dir(sourcePath)
	}
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		return nil
	})
	if closeErr := tw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gz.Close(); err == nil {
		err = closeErr
	}
	return err
}

func safeTag(value string) string {
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, value)
	if value == "" {
		return "checkpoint"
	}
	return value
}

func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open checkpoint for digest: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat checkpoint for digest: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", 0, fmt.Errorf("digest checkpoint: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), info.Size(), nil
}

var _ TransferProvider = (*RegistryTransferProvider)(nil)
