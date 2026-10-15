package transfer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// CRIORegistryTransferProvider converts a Kubelet checkpoint archive into the
// OCI image format CRI-O recognizes for restore. It requires buildah on the
// manager host and a registry reachable by target nodes.
type CRIORegistryTransferProvider struct {
	Repository    string
	ContainerName string
	run           CommandRunner
}

func NewCRIORegistryTransferProvider(repository string, run CommandRunner) *CRIORegistryTransferProvider {
	if run == nil {
		run = DefaultCommandRunner
	}
	return &CRIORegistryTransferProvider{Repository: strings.TrimRight(strings.TrimSpace(repository), "/"), run: run}
}

func (p *CRIORegistryTransferProvider) WithContainerName(name string) *CRIORegistryTransferProvider {
	p.ContainerName = strings.TrimSpace(name)
	return p
}

func (p *CRIORegistryTransferProvider) Transfer(ctx context.Context, sourcePath, targetNode, _ string, _ string) (*TransferResult, error) {
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
	if p == nil || p.run == nil || p.Repository == "" {
		return finish(fmt.Errorf("CRI-O registry transfer is not configured"))
	}
	name := p.ContainerName
	if name == "" {
		name = filepath.Base(sourcePath)
	}
	ref := fmt.Sprintf("%s:checkpoint-%d", p.Repository, start.UnixNano())
	out, err := p.run(ctx, "buildah", "from", "scratch")
	if err != nil {
		return finish(fmt.Errorf("buildah from failed: %w", err))
	}
	container := strings.TrimSpace(string(out))
	cleanup := func() { _, _ = p.run(context.Background(), "buildah", "rm", container) }
	defer cleanup()
	if _, err = p.run(ctx, "buildah", "add", container, sourcePath, "/"); err != nil {
		return finish(fmt.Errorf("buildah add checkpoint failed: %w", err))
	}
	if _, err = p.run(ctx, "buildah", "config", "--annotation", "io.kubernetes.cri-o.annotations.checkpoint.name="+name, container); err != nil {
		return finish(fmt.Errorf("buildah checkpoint annotation failed: %w", err))
	}
	if _, err = p.run(ctx, "buildah", "commit", container, ref); err != nil {
		return finish(fmt.Errorf("buildah commit checkpoint image failed: %w", err))
	}
	if _, err = p.run(ctx, "buildah", "push", ref, "docker://"+ref); err != nil {
		return finish(fmt.Errorf("buildah push checkpoint image failed: %w", err))
	}
	result.DestinationPath = "oci://" + ref
	return finish(nil)
}
