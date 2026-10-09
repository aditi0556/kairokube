package checkpoint

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// KubeletCheckpointResponse represents the JSON response from the Kubelet checkpoint endpoint.
type KubeletCheckpointResponse struct {
	Items []string `json:"items"`
}

// KubeletFCCConfig holds configuration for calling the Kubelet checkpoint API.
type KubeletFCCConfig struct {
	// UseNodeProxy if true calls https://<apiserver>/api/v1/nodes/<node>/proxy/checkpoint/...
	UseNodeProxy bool
	// KubeletPort defaults to 10250
	KubeletPort int
	// Scheme defaults to https
	Scheme string
	// InsecureSkipVerify for clusters using self-signed kubelet serving certificates
	InsecureSkipVerify bool
	// CheckpointDir on the node host
	CheckpointDir string
}

// KubeletFCCProvider interacts with the official Kubernetes Forensic Container Checkpointing (FCC) API.
// Required cluster configuration:
//  1. Kubelet feature gate: --feature-gates=ContainerCheckpoint=true
//  2. Container runtime with CRIU support (e.g. CRI-O or containerd with CRIU)
//  3. RBAC permission: nodes/proxy create/get or kubelet authentication
type KubeletFCCProvider struct {
	clientset  kubernetes.Interface
	restConfig *rest.Config
	config     KubeletFCCConfig
	httpClient *http.Client
}

// NewKubeletFCCProvider creates a new KubeletFCCProvider with the given Kubernetes client and config.
func NewKubeletFCCProvider(clientset kubernetes.Interface, restConfig *rest.Config, cfg KubeletFCCConfig) *KubeletFCCProvider {
	if cfg.KubeletPort == 0 {
		cfg.KubeletPort = 10250
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "https"
	}
	if cfg.CheckpointDir == "" {
		cfg.CheckpointDir = "/var/lib/kubelet/checkpoints"
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		},
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	return &KubeletFCCProvider{
		clientset:  clientset,
		restConfig: restConfig,
		config:     cfg,
		httpClient: httpClient,
	}
}

// CreateCheckpoint initiates an FCC request to the node hosting the pod.
func (p *KubeletFCCProvider) CreateCheckpoint(ctx context.Context, namespace, podName, containerName string) (*CheckpointResult, error) {
	if p.clientset == nil {
		return nil, fmt.Errorf("kubernetes clientset is not initialized")
	}

	start := time.Now()

	// 1. Fetch Pod to discover which Node hosts it and discover container name if empty
	pod, err := p.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pod %s/%s: %w", namespace, podName, err)
	}

	if containerName == "" && len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}
	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return nil, fmt.Errorf("pod %s/%s is not assigned to a node", namespace, podName)
	}

	var checkpointPath string

	if p.config.UseNodeProxy && p.restConfig != nil {
		// Method A: API Server Kubelet Proxy
		// POST /api/v1/nodes/<nodeName>/proxy/checkpoint/<namespace>/<podName>/<containerName>
		req := p.clientset.CoreV1().RESTClient().Post().
			Resource("nodes").
			Name(nodeName).
			SubResource("proxy").
			Suffix("checkpoint", namespace, podName, containerName)

		raw, err := req.DoRaw(ctx)
		if err != nil {
			return nil, fmt.Errorf("kubelet proxy checkpoint request failed for %s/%s/%s: %w (ensure ContainerCheckpoint feature gate is enabled on node %s)",
				namespace, podName, containerName, err, nodeName)
		}

		var resp KubeletCheckpointResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse kubelet checkpoint response: %w (raw: %s)", err, string(raw))
		}
		if len(resp.Items) == 0 {
			return nil, fmt.Errorf("kubelet returned empty checkpoint artifact list")
		}
		checkpointPath = resp.Items[0]
	} else {
		// Method B: Direct Node Kubelet API
		node, err := p.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get node %s: %w", nodeName, err)
		}

		nodeIP := ""
		for _, addr := range node.Status.Addresses {
			if addr.Type == "InternalIP" {
				nodeIP = addr.Address
				break
			}
		}
		if nodeIP == "" && len(node.Status.Addresses) > 0 {
			nodeIP = node.Status.Addresses[0].Address
		}
		if nodeIP == "" {
			return nil, fmt.Errorf("cannot find IP address for node %s", nodeName)
		}

		url := fmt.Sprintf("%s://%s:%d/checkpoint/%s/%s/%s",
			p.config.Scheme, nodeIP, p.config.KubeletPort, namespace, podName, containerName)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to construct kubelet request: %w", err)
		}

		if p.restConfig != nil && p.restConfig.BearerToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+p.restConfig.BearerToken)
		}

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Kubelet at %s: %w", url, err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("kubelet checkpoint API returned HTTP %d: %s (ensure --feature-gates=ContainerCheckpoint=true is enabled)", resp.StatusCode, string(body))
		}

		var ckResp KubeletCheckpointResponse
		if err := json.Unmarshal(body, &ckResp); err != nil {
			return nil, fmt.Errorf("failed to decode kubelet response: %w", err)
		}
		if len(ckResp.Items) == 0 {
			return nil, fmt.Errorf("kubelet returned empty checkpoint items list")
		}
		checkpointPath = ckResp.Items[0]
	}

	duration := time.Since(start)

	// A path returned by the Kubelet is node-local. Do not invent size/checksum
	// metadata when this process cannot read the artifact.
	fi, err := os.Stat(checkpointPath)
	if err != nil {
		return nil, fmt.Errorf("checkpoint created at node-local path %q but not readable by manager: %w; configure shared artifact access and an explicit transfer implementation", checkpointPath, err)
	}
	checksum, err := fileSHA256(checkpointPath)
	if err != nil {
		return nil, fmt.Errorf("failed to checksum checkpoint artifact %q: %w", checkpointPath, err)
	}

	return &CheckpointResult{
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,
		NodeName:      nodeName,
		FilePath:      checkpointPath,
		SizeBytes:     fi.Size(),
		CreatedAt:     time.Now().UTC(),
		Duration:      duration,
		Checksum:      checksum,
	}, nil
}

// RestoreCheckpoint attempts to restore the container state on the target environment.
func (p *KubeletFCCProvider) RestoreCheckpoint(ctx context.Context, checkpoint *CheckpointResult, target Target) error {
	if err := p.ValidateRestore(ctx, checkpoint, target); err != nil {
		return err
	}
	return nil
}

// ValidateRestore reports that this provider has no runtime-specific restore implementation.
func (p *KubeletFCCProvider) ValidateRestore(ctx context.Context, checkpoint *CheckpointResult, target Target) error {
	_ = ctx
	if checkpoint == nil {
		return fmt.Errorf("checkpoint result cannot be nil")
	}
	return fmt.Errorf("Kubelet FCC creates checkpoint archives but this provider does not implement CRI/runtime restore; refusing to report restore success for target %s/%s on %s", target.Namespace, target.PodName, target.NodeName)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Ensure interface compliance
var _ CheckpointProvider = (*KubeletFCCProvider)(nil)
