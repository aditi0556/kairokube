package checkpoint

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DockerRestorer restores a Docker-native CRIU checkpoint into an existing
// stopped container. Docker stores checkpoints in a directory and restores
// them with `docker start --checkpoint ...`; it does not accept a generic
// `container restore --import` archive syntax.
type DockerRestorer struct {
	Username string
	Password string
	run      CommandRunner
	runInput dockerCommandRunnerWithInput
}

type dockerCommandRunnerWithInput func(ctx context.Context, name, input string, args ...string) ([]byte, error)

// WithRegistryCredentials configures ORAS credentials for registry-backed
// checkpoint pulls. Existing ORAS auth configuration is used when empty.
func (r *DockerRestorer) WithRegistryCredentials(username, password string) *DockerRestorer {
	r.Username, r.Password = username, password
	return r
}

// ErrDockerRestoreUnavailable is returned when no Docker command runner exists.
var ErrDockerRestoreUnavailable = errors.New("docker restore helper has no command runner configured")

// NewDockerRestorer creates a Docker restorer. If run is nil, the real Docker
// CLI command runner is used.
func NewDockerRestorer(run CommandRunner) *DockerRestorer {
	usingDefaultRunner := run == nil
	if run == nil {
		run = DefaultCommandRunner
	}
	inputRunner := dockerDefaultCommandRunnerWithInput
	if !usingDefaultRunner {
		inputRunner = nil
	}
	return &DockerRestorer{run: run, runInput: inputRunner}
}

var dockerDefaultCommandRunnerWithInput dockerCommandRunnerWithInput = func(ctx context.Context, name, input string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.CombinedOutput()
}

// RestoreArchive restores the Docker checkpoint represented by checkpointPath.
// checkpointPath may point to the checkpoint directory itself or to a file
// inside that directory. The target container must already exist and be stopped.
func (r *DockerRestorer) RestoreArchive(ctx context.Context, checkpointPath, containerID string) (string, error) {
	if r == nil || r.run == nil {
		return "", ErrDockerRestoreUnavailable
	}
	checkpointPath = strings.TrimSpace(checkpointPath)
	containerID = strings.TrimSpace(containerID)
	if checkpointPath == "" || containerID == "" {
		return "", errors.New("checkpoint path and container ID are required")
	}

	localPath, cleanup, err := r.materializeCheckpoint(ctx, checkpointPath)
	if err != nil {
		return "", err
	}
	defer cleanup()
	info, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("docker checkpoint path not readable: %w", err)
	}
	checkpointName := filepath.Base(filepath.Clean(localPath))
	checkpointDir := filepath.Dir(localPath)
	if !info.IsDir() {
		checkpointName = filepath.Base(localPath)
	}
	if checkpointName == "." || checkpointName == string(filepath.Separator) || checkpointName == "" {
		return "", errors.New("docker checkpoint name is empty")
	}

	out, err := r.run(ctx, "docker", "start", "--checkpoint", checkpointName, "--checkpoint-dir", checkpointDir, containerID)
	if err != nil {
		return "", fmt.Errorf("docker start checkpoint restore failed: %w", err)
	}
	if id := strings.TrimSpace(string(out)); id != "" {
		return id, nil
	}
	return containerID, nil
}

func (r *DockerRestorer) materializeCheckpoint(ctx context.Context, checkpointPath string) (string, func(), error) {
	if !strings.HasPrefix(checkpointPath, "oci://") {
		return checkpointPath, func() {}, nil
	}
	ref := strings.TrimPrefix(checkpointPath, "oci://")
	workDir, err := os.MkdirTemp("", "kairokube-checkpoint-pull-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create checkpoint pull directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }
	if err := r.loginRegistry(ctx, ref); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if _, err := r.run(ctx, "oras", "pull", ref, "--output", workDir); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("oras pull failed: %w", err)
	}
	archivePath, err := findCheckpointArchive(workDir)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	extractedDir := filepath.Join(workDir, "extracted")
	if err := os.Mkdir(extractedDir, 0700); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("create extracted checkpoint directory: %w", err)
	}
	if err := extractCheckpointArchive(archivePath, extractedDir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	entries, err := os.ReadDir(extractedDir)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("inspect extracted checkpoint: %w", err)
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(extractedDir, entries[0].Name()), cleanup, nil
	}
	return extractedDir, cleanup, nil
}

func (r *DockerRestorer) loginRegistry(ctx context.Context, ref string) error {
	if r.Username == "" && r.Password == "" {
		return nil
	}
	if r.Username == "" || r.Password == "" {
		return fmt.Errorf("registry username and password must be provided together")
	}
	host := strings.SplitN(ref, "/", 2)[0]
	if r.runInput == nil {
		return fmt.Errorf("registry credentials require the default command runner")
	}
	if _, err := r.runInput(ctx, "oras", r.Password+"\n", "login", "-u", r.Username, "--password-stdin", host); err != nil {
		return fmt.Errorf("oras login failed: %w", err)
	}
	return nil
}

func findCheckpointArchive(root string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() && (strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".tgz") || strings.HasSuffix(path, ".tar")) {
			found = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("find pulled checkpoint archive: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("ORAS artifact contains no checkpoint archive")
	}
	return found, nil
}

func extractCheckpointArchive(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open pulled checkpoint archive: %w", err)
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tgz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("read pulled checkpoint gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read pulled checkpoint archive: %w", err)
		}
		target, err := safeArchivePath(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func safeArchivePath(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("checkpoint archive contains unsafe path %q", name)
	}
	return filepath.Join(root, clean), nil
}

var _ NodeRestorer = (*DockerRestorer)(nil)
