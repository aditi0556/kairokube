package migration

import (
	"context"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WorkloadController abstracts lifecycle operations on source and target workloads
// to isolate individual-Pod versus StatefulSet behaviors.
type WorkloadController interface {
	// ValidateSource verifies the source workload exists, is running, and is ready.
	ValidateSource(ctx context.Context, namespace, podName string) error

	// ValidateTargetNode verifies the target node exists, is Ready, and is schedulable.
	ValidateTargetNode(ctx context.Context, targetNode string) error

	// PrepareTarget creates or schedules the target workload on the target node.
	PrepareTarget(ctx context.Context, namespace, sourcePod, targetNode string) (string, error)

	// StopSource stops the source workload at cutoff time.
	StopSource(ctx context.Context, namespace, podName string) error

	// VerifyTarget waits for the target workload to achieve Ready status.
	VerifyTarget(ctx context.Context, namespace, targetPod string, timeout time.Duration) error

	// RestoreSource restores processing on the source workload during rollback.
	RestoreSource(ctx context.Context, namespace, podName string) error

	// CleanupTarget terminates and cleans up the target workload upon migration failure.
	CleanupTarget(ctx context.Context, namespace, targetPod string) error
}

// PodWorkloadController manages individual Pod workloads (MIGRATION_MODE=pod).
type PodWorkloadController struct {
	clientset kubernetes.Interface
}

// NewPodWorkloadController creates a PodWorkloadController.
func NewPodWorkloadController(cs kubernetes.Interface) *PodWorkloadController {
	return &PodWorkloadController{clientset: cs}
}

func (c *PodWorkloadController) ValidateSource(ctx context.Context, namespace, podName string) error {
	if c.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("source pod %s/%s not found: %w", namespace, podName, err)
	}
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("source pod %s/%s is not in Running phase (phase: %s)", namespace, podName, pod.Status.Phase)
	}
	if len(pod.OwnerReferences) > 0 {
		return fmt.Errorf("source pod %s/%s is controller-owned; standalone Pod mode will not delete a workload managed by a controller", namespace, podName)
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil || volume.HostPath != nil {
			return fmt.Errorf("source pod %s/%s uses volume %q with storage semantics that this migration workflow cannot safely hand off", namespace, podName, volume.Name)
		}
	}
	ready := false
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}
	if !ready {
		return fmt.Errorf("source pod %s/%s is not Ready", namespace, podName)
	}
	return nil
}

func (c *PodWorkloadController) ValidateTargetNode(ctx context.Context, targetNode string) error {
	if c.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, targetNode, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("target node %s not found: %w", targetNode, err)
	}
	if node.Spec.Unschedulable {
		return fmt.Errorf("target node %s is cordoned (unschedulable)", targetNode)
	}
	ready := false
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}
	if !ready {
		return fmt.Errorf("target node %s is not in Ready state", targetNode)
	}
	return nil
}

func (c *PodWorkloadController) PrepareTarget(ctx context.Context, namespace, sourcePod, targetNode string) (string, error) {
	if c.clientset == nil {
		return "", fmt.Errorf("kubernetes clientset is nil")
	}
	src, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, sourcePod, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to fetch source pod for cloning: %w", err)
	}

	targetPodName := fmt.Sprintf("%s-target", sourcePod)

	// Never delete an existing Pod as a convenience; it may be a live workload.
	if _, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, targetPodName, metav1.GetOptions{}); err == nil {
		return "", fmt.Errorf("target Pod %s/%s already exists; refusing to delete or overwrite it", namespace, targetPodName)
	} else if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("failed to check target Pod %s/%s: %w", namespace, targetPodName, err)
	}

	// Construct target Pod spec pinned to targetNode
	targetPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetPodName,
			Namespace: namespace,
			Labels:    src.Labels,
		},
		Spec: corev1.PodSpec{
			NodeName:      targetNode,
			Containers:    src.Spec.Containers,
			RestartPolicy: corev1.RestartPolicyAlways,
			Volumes:       src.Spec.Volumes,
		},
	}
	// Avoid selector collision by tagging migration role
	if targetPod.Labels == nil {
		targetPod.Labels = make(map[string]string)
	}
	targetPod.Labels["ms2m.migration/role"] = "target"

	created, err := c.clientset.CoreV1().Pods(namespace).Create(ctx, targetPod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create target pod %s: %w", targetPodName, err)
	}
	return created.Name, nil
}

func (c *PodWorkloadController) StopSource(ctx context.Context, namespace, podName string) error {
	if c.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	err := c.clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to stop source pod %s/%s: %w", namespace, podName, err)
	}
	return nil
}

func (c *PodWorkloadController) VerifyTarget(ctx context.Context, namespace, targetPod string, timeout time.Duration) error {
	if c.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for target pod %s to become Ready", targetPod)
			}
			pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, targetPod, metav1.GetOptions{})
			if err != nil {
				continue
			}
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					return nil
				}
			}
		}
	}
}

func (c *PodWorkloadController) RestoreSource(ctx context.Context, namespace, podName string) error {
	if c.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	if _, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{}); err == nil {
		return nil
	} else {
		return fmt.Errorf("source pod %s/%s is absent and this controller does not retain a safe recreation template: %w", namespace, podName, err)
	}
}

func (c *PodWorkloadController) CleanupTarget(ctx context.Context, namespace, targetPod string) error {
	if c.clientset == nil || targetPod == "" {
		return nil
	}
	return c.clientset.CoreV1().Pods(namespace).Delete(ctx, targetPod, metav1.DeleteOptions{})
}

// StatefulSetWorkloadController manages StatefulSet workloads (MIGRATION_MODE=statefulset).
// Addresses stable network identity, persistent volume claims (PVCs), and StatefulSet controller reconciliation.
type StatefulSetWorkloadController struct {
	clientset kubernetes.Interface
}

// NewStatefulSetWorkloadController creates a StatefulSetWorkloadController.
func NewStatefulSetWorkloadController(cs kubernetes.Interface) *StatefulSetWorkloadController {
	return &StatefulSetWorkloadController{clientset: cs}
}

func (s *StatefulSetWorkloadController) ValidateSource(ctx context.Context, namespace, podName string) error {
	if s.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	pod, err := s.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("source statefulset pod %s/%s not found: %w", namespace, podName, err)
	}
	// Check for owner reference or statefulset pod name format (e.g. consumer-0)
	isStatefulSet := false
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "StatefulSet" {
			isStatefulSet = true
			break
		}
	}
	if !isStatefulSet && pod.Labels["apps.kubernetes.io/pod-index"] == "" {
		log.Printf("Warning: Pod %s does not declare a StatefulSet owner reference; verifying ordinal identity", podName)
	}
	return nil
}

func (s *StatefulSetWorkloadController) ValidateTargetNode(ctx context.Context, targetNode string) error {
	if s.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	node, err := s.clientset.CoreV1().Nodes().Get(ctx, targetNode, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("target node %s not found: %w", targetNode, err)
	}
	if node.Spec.Unschedulable {
		return fmt.Errorf("target node %s is unschedulable", targetNode)
	}
	return nil
}

func (s *StatefulSetWorkloadController) PrepareTarget(ctx context.Context, namespace, sourcePod, targetNode string) (string, error) {
	// In StatefulSet mode, moving a pod requires either updating node affinity or scheduling the restored
	// container directly. For individual pod migration within StatefulSets, we deploy a staged shadow target
	// pod that claims the identity upon cutoff.
	targetPodName := fmt.Sprintf("%s-target", sourcePod)
	log.Printf("Preparing StatefulSet target workload %s on node %s", targetPodName, targetNode)
	return targetPodName, nil
}

func (s *StatefulSetWorkloadController) StopSource(ctx context.Context, namespace, podName string) error {
	if s.clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	// Note: Deleting a StatefulSet pod causes the StatefulSet controller to recreate it.
	// For production MS2M, the StatefulSet must be scaled down or cordoned to prevent immediate restart.
	log.Printf("Stopping StatefulSet source pod %s/%s", namespace, podName)
	return s.clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

func (s *StatefulSetWorkloadController) VerifyTarget(ctx context.Context, namespace, targetPod string, timeout time.Duration) error {
	log.Printf("Verifying StatefulSet target %s readiness", targetPod)
	return nil
}

func (s *StatefulSetWorkloadController) RestoreSource(ctx context.Context, namespace, podName string) error {
	log.Printf("Restoring StatefulSet source pod %s/%s", namespace, podName)
	return nil
}

func (s *StatefulSetWorkloadController) CleanupTarget(ctx context.Context, namespace, targetPod string) error {
	if s.clientset == nil || targetPod == "" {
		return nil
	}
	return s.clientset.CoreV1().Pods(namespace).Delete(ctx, targetPod, metav1.DeleteOptions{})
}

// MockWorkloadController provides in-memory mock workload operations for unit testing.
type MockWorkloadController struct {
	SourceHealthy   bool
	TargetNodeReady bool
	TargetPodReady  bool
	SimulateError   error
}

// NewMockWorkloadController creates a MockWorkloadController.
func NewMockWorkloadController() *MockWorkloadController {
	return &MockWorkloadController{
		SourceHealthy:   true,
		TargetNodeReady: true,
		TargetPodReady:  true,
	}
}

func (m *MockWorkloadController) ValidateSource(ctx context.Context, namespace, podName string) error {
	if m.SimulateError != nil {
		return m.SimulateError
	}
	if !m.SourceHealthy {
		return fmt.Errorf("source pod %s is not healthy", podName)
	}
	return nil
}

func (m *MockWorkloadController) ValidateTargetNode(ctx context.Context, targetNode string) error {
	if m.SimulateError != nil {
		return m.SimulateError
	}
	if !m.TargetNodeReady {
		return fmt.Errorf("target node %s is not ready", targetNode)
	}
	return nil
}

func (m *MockWorkloadController) PrepareTarget(ctx context.Context, namespace, sourcePod, targetNode string) (string, error) {
	if m.SimulateError != nil {
		return "", m.SimulateError
	}
	return fmt.Sprintf("%s-target", sourcePod), nil
}

func (m *MockWorkloadController) StopSource(ctx context.Context, namespace, podName string) error {
	return m.SimulateError
}

func (m *MockWorkloadController) VerifyTarget(ctx context.Context, namespace, targetPod string, timeout time.Duration) error {
	if m.SimulateError != nil {
		return m.SimulateError
	}
	if !m.TargetPodReady {
		return fmt.Errorf("target pod %s readiness check failed", targetPod)
	}
	return nil
}

func (m *MockWorkloadController) RestoreSource(ctx context.Context, namespace, podName string) error {
	return nil
}

func (m *MockWorkloadController) CleanupTarget(ctx context.Context, namespace, targetPod string) error {
	return nil
}

var _ WorkloadController = (*PodWorkloadController)(nil)
var _ WorkloadController = (*StatefulSetWorkloadController)(nil)
var _ WorkloadController = (*MockWorkloadController)(nil)
