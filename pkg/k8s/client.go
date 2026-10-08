// Package k8s provides Kubernetes client helpers used by the MS2M
// migration manager.
//
// The package wraps the official Kubernetes client-go library and
// provides operations required during stateful microservice migration,
// such as accessing the Kubernetes API and stopping a migrated Pod.
//
// The Migration Manager is expected to run inside the Kubernetes cluster
// and authenticate using the service account assigned to its Pod.
package k8s

import (
	"context"
	"fmt"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Client wraps a Kubernetes clientset and provides helper operations
// used by the MS2M migration workflow.
//
// Client should normally be created with NewClient while running inside
// a Kubernetes cluster. The embedded Clientset is used to communicate
// with the Kubernetes API server.
type Client struct {
	Clientset *kubernetes.Clientset
}

// NewClient creates a Kubernetes client using the in-cluster
// configuration.
//
// In-cluster configuration uses the Kubernetes service account and
// configuration automatically provided to a Pod running inside a
// Kubernetes cluster.
//
// The Migration Manager therefore does not need to store a kubeconfig
// file inside its container.
//
// NewClient returns an error when the application is not running inside
// a Kubernetes cluster or when the Kubernetes client cannot be created.
func NewClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to load in-cluster Kubernetes configuration: %w",
			err,
		)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create Kubernetes clientset: %w",
			err,
		)
	}

	return &Client{
		Clientset: clientset,
	}, nil
}

// RequestCheckpoint requests creation of a checkpoint for the specified
// Kubernetes Pod.
//
// The MS2M migration workflow uses Kubernetes checkpointing to capture
// the execution state of the containers running inside the source Pod
// before the checkpoint is transferred to the target node.
//
// The current implementation is intentionally a placeholder because
// the standard client-go Clientset does not expose the experimental
// Forensic Container Checkpointing (FCC) operation used by the paper.
// The actual implementation needs to communicate with the appropriate
// Kubernetes/Kubelet checkpoint endpoint supported by the cluster.
//
// The method currently validates its arguments and logs the requested
// operation but does not create a real checkpoint.
func (c *Client) RequestCheckpoint(
	ctx context.Context,
	podName string,
	namespace string,
) error {
	if c == nil || c.Clientset == nil {
		return fmt.Errorf("Kubernetes client is not initialized")
	}

	if podName == "" {
		return fmt.Errorf("Pod name cannot be empty")
	}

	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	log.Printf(
		"Requesting FCC checkpoint for Pod %q in namespace %q",
		podName,
		namespace,
	)

	// TODO:
	//
	// Implement the actual FCC checkpoint request here.
	//
	// The standard Kubernetes API client does not provide a direct
	// clientset method for the experimental Kubelet checkpoint API.
	// The implementation must therefore call the checkpoint endpoint
	// exposed by the Kubernetes/Kubelet version used by the cluster.
	//
	// The checkpoint request should:
	//
	// 1. Identify the source Pod.
	// 2. Determine the node hosting the Pod.
	// 3. Identify the containers that need to be checkpointed.
	// 4. Request checkpoint creation from the appropriate Kubelet.
	// 5. Wait for the checkpoint operation to complete.
	// 6. Return an error if checkpoint creation fails.
	//
	// Do not silently return success once this integration is added.

	return nil
}

// StopPod deletes the specified Kubernetes Pod.
//
// This operation is used during migration finalization after the target
// service has been restored and synchronized with the source service.
//
// Deleting the source Pod is consistent with the migration workflow
// described in the paper, where the source service is permanently
// stopped after the target has synchronized its state.
func (c *Client) StopPod(
	ctx context.Context,
	podName string,
	namespace string,
) error {
	if c == nil || c.Clientset == nil {
		return fmt.Errorf("Kubernetes client is not initialized")
	}

	if podName == "" {
		return fmt.Errorf("Pod name cannot be empty")
	}

	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	log.Printf(
		"Stopping Pod %q in namespace %q",
		podName,
		namespace,
	)

	err := c.Clientset.CoreV1().
		Pods(namespace).
		Delete(ctx, podName, metav1.DeleteOptions{})

	if err != nil {
		return fmt.Errorf(
			"failed to delete Pod %q in namespace %q: %w",
			podName,
			namespace,
			err,
		)
	}

	log.Printf(
		"Pod %q in namespace %q deletion requested successfully",
		podName,
		namespace,
	)

	return nil
}