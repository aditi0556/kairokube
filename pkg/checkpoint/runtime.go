package checkpoint

import (
	"fmt"
	"strings"
)

// RuntimeInfo describes the CRI runtime reported by a Kubernetes node.
type RuntimeInfo struct {
	Raw       string `json:"raw"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Supported bool   `json:"supported"`
}

// ParseRuntimeInfo parses values such as "crio://1.35.0" and
// "containerd://2.1.0" from NodeInfo.ContainerRuntimeVersion.
func ParseRuntimeInfo(raw string) RuntimeInfo {
	info := RuntimeInfo{Raw: strings.TrimSpace(raw)}
	parts := strings.SplitN(info.Raw, "://", 2)
	if len(parts) == 2 {
		info.Name = strings.ToLower(strings.TrimSpace(parts[0]))
		info.Version = strings.TrimSpace(parts[1])
	} else {
		info.Name = strings.ToLower(info.Raw)
	}
	info.Supported = info.Name == "crio" || info.Name == "containerd"
	return info
}

// ValidateCheckpointRuntime rejects runtimes that cannot be used with the
// Kubernetes Kubelet checkpoint API. Kernel and CRIU capability checks still
// happen when the checkpoint is requested.
func ValidateCheckpointRuntime(raw string) error {
	info := ParseRuntimeInfo(raw)
	if info.Name == "" {
		return fmt.Errorf("node does not report a container runtime")
	}
	if info.Name == "docker" || info.Name == "podman" {
		return fmt.Errorf("runtime %q is not a Kubernetes CRI restore runtime; use CRI-O or containerd with CRIU", info.Name)
	}
	if !info.Supported {
		return fmt.Errorf("unsupported Kubernetes checkpoint runtime %q; use CRI-O or containerd with CRIU", info.Raw)
	}
	return nil
}
