// Package k8s provides Kubernetes client helpers used by the MS2M migration manager.
package k8s

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps a Kubernetes clientset and configuration.
type Client struct {
	Clientset  *kubernetes.Clientset
	RestConfig *rest.Config
}

// NewClient creates a Kubernetes client by attempting in-cluster configuration first,
// and falling back to KUBECONFIG / ~/.kube/config if running outside the cluster.
func NewClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Out of cluster fallback
		kubeconfigPath := os.Getenv("KUBECONFIG")
		if kubeconfigPath == "" {
			if home, hErr := os.UserHomeDir(); hErr == nil {
				kubeconfigPath = filepath.Join(home, ".kube", "config")
			}
		}

		if kubeconfigPath != "" {
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to load Kubernetes config (tried in-cluster and kubeconfig %q): %w", kubeconfigPath, err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	return &Client{
		Clientset:  clientset,
		RestConfig: config,
	}, nil
}

// NewClientWithConfig creates a Client with an explicit rest.Config and clientset.
func NewClientWithConfig(config *rest.Config, clientset *kubernetes.Clientset) *Client {
	return &Client{
		Clientset:  clientset,
		RestConfig: config,
	}
}

// GetPod retrieves the specified Pod.
func (c *Client) GetPod(ctx context.Context, namespace, podName string) (*corev1.Pod, error) {
	if c == nil || c.Clientset == nil {
		return nil, fmt.Errorf("Kubernetes client is not initialized")
	}
	return c.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
}

// GetNode retrieves the specified Node.
func (c *Client) GetNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	if c == nil || c.Clientset == nil {
		return nil, fmt.Errorf("Kubernetes client is not initialized")
	}
	return c.Clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
}

// StopPod deletes the specified Kubernetes Pod.
func (c *Client) StopPod(ctx context.Context, podName string, namespace string) error {
	if c == nil || c.Clientset == nil {
		return fmt.Errorf("Kubernetes client is not initialized")
	}
	if podName == "" {
		return fmt.Errorf("pod name cannot be empty")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	log.Printf("Stopping Pod %s in namespace %s", podName, namespace)
	err := c.Clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete Pod %s in namespace %s: %w", podName, namespace, err)
	}
	return nil
}

// RequestCheckpoint delegates checkpoint creation to a dedicated CheckpointProvider.
// Standalone client does not silently fake checkpoint success.
func (c *Client) RequestCheckpoint(ctx context.Context, podName string, namespace string) error {
	if c == nil || c.Clientset == nil {
		return fmt.Errorf("Kubernetes client is not initialized")
	}
	// Per MS2M specifications, FCC checkpointing must be requested through CheckpointProvider
	return fmt.Errorf("RequestCheckpoint must be invoked through CheckpointProvider (e.g. KubeletFCCProvider) rather than standard client-go API")
}
