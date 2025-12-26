/*
Copyright (c) 2024 Nik Ogura

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package podboard

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodInfo represents pod information for the dashboard.
type PodInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	ImageTag  string            `json:"imageTag"`
	Status    string            `json:"status"`
	Ready     string            `json:"ready"`
	Restarts  int32             `json:"restarts"`
	Age       string            `json:"age"`
	Node      string            `json:"node"`
	IP        string            `json:"ip"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// PodService handles pod-related operations.
type PodService struct {
	kubeConfigService *KubeConfigService
	logger            *zap.Logger
}

// NewPodService creates a new pod service.
func NewPodService(kubeConfigService *KubeConfigService, logger *zap.Logger) *PodService {
	return &PodService{
		kubeConfigService: kubeConfigService,
		logger:            logger,
	}
}

// GetPods retrieves pods from the specified namespace with optional label selector and cluster.
// Supports regex patterns in label selectors using the =~ operator (e.g., "app=~nginx.*").
// Use namespace="all" to retrieve pods from all namespaces.
func (ps *PodService) GetPods(ctx context.Context, clusterName, namespace, labelSelector string) ([]PodInfo, error) {
	client, err := ps.getClient(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client: %w", err)
	}

	// Handle "all" namespace by using empty string for Kubernetes API
	queryNamespace := namespace
	if namespace == "all" {
		queryNamespace = ""
	}

	var pods *corev1.PodList

	// Check if this is a regex selector
	if strings.Contains(labelSelector, "=~") {
		// For regex selectors, we need to fetch all pods and filter manually
		pods, err = client.CoreV1().Pods(queryNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			ps.logger.Error("Failed to list pods for regex filtering", zap.Error(err), zap.String("cluster", clusterName), zap.String("namespace", namespace))
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}
	} else {
		// Use standard Kubernetes label selector
		listOptions := metav1.ListOptions{}
		if labelSelector != "" {
			listOptions.LabelSelector = labelSelector
		}

		pods, err = client.CoreV1().Pods(queryNamespace).List(ctx, listOptions)
		if err != nil {
			ps.logger.Error("Failed to list pods", zap.Error(err), zap.String("cluster", clusterName), zap.String("namespace", namespace), zap.String("labelSelector", labelSelector))
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}
	}

	var podInfos []PodInfo
	for _, pod := range pods.Items {
		// Apply regex filtering if needed
		if strings.Contains(labelSelector, "=~") {
			if !ps.matchesRegexSelector(pod.Labels, labelSelector) {
				continue
			}
		}

		podInfo := ps.podToPodInfo(&pod)
		podInfos = append(podInfos, podInfo)
	}

	return podInfos, nil
}

// GetNamespaces retrieves all namespaces for the given cluster.
func (ps *PodService) GetNamespaces(ctx context.Context, clusterName string) ([]string, error) {
	client, err := ps.getClient(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client: %w", err)
	}

	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		ps.logger.Error("Failed to list namespaces", zap.Error(err), zap.String("cluster", clusterName))
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var names []string
	for _, ns := range namespaces.Items {
		names = append(names, ns.Name)
	}

	return names, nil
}

// getClient returns a Kubernetes client for the given cluster
func (ps *PodService) getClient(clusterName string) (kubernetes.Interface, error) {
	if ps.kubeConfigService.IsInCluster() {
		// When running in cluster, use in-cluster config regardless of cluster
		return ps.kubeConfigService.CreateClientForCluster("")
	}

	if clusterName == "" {
		// If no cluster specified, try to get current cluster
		currentCluster, err := ps.kubeConfigService.GetCurrentCluster()
		if err != nil {
			return nil, fmt.Errorf("no cluster specified and failed to get current cluster: %w", err)
		}
		clusterName = currentCluster
	}

	return ps.kubeConfigService.CreateClientForCluster(clusterName)
}

// DeletePod deletes a pod by name in the specified namespace and cluster
func (ps *PodService) DeletePod(ctx context.Context, clusterName, namespace, podName string) error {
	client, err := ps.getClient(clusterName)
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes client: %w", err)
	}

	err = client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		ps.logger.Error("Failed to delete pod", zap.Error(err), zap.String("cluster", clusterName), zap.String("namespace", namespace), zap.String("pod", podName))
		return fmt.Errorf("failed to delete pod %s/%s: %w", namespace, podName, err)
	}

	ps.logger.Info("Pod deleted successfully", zap.String("cluster", clusterName), zap.String("namespace", namespace), zap.String("pod", podName))
	return nil
}

func (ps *PodService) podToPodInfo(pod *corev1.Pod) PodInfo {
	// Calculate ready containers
	readyContainers := 0
	totalContainers := len(pod.Spec.Containers)
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			readyContainers++
		}
	}

	// Calculate restart count
	var restarts int32
	for _, status := range pod.Status.ContainerStatuses {
		restarts += status.RestartCount
	}

	// Calculate age
	age := time.Since(pod.CreationTimestamp.Time)
	ageStr := formatDuration(age)

	// Get pod status - check container states for more accurate status
	status := ps.getPodStatus(pod)

	// Extract image tag from first container
	imageTag := extractImageTag(pod)

	return PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		ImageTag:  imageTag,
		Status:    status,
		Ready:     fmt.Sprintf("%d/%d", readyContainers, totalContainers),
		Restarts:  restarts,
		Age:       ageStr,
		Node:      pod.Spec.NodeName,
		IP:        pod.Status.PodIP,
		Labels:    pod.Labels,
	}
}

// getPodStatus returns the most accurate status for a pod by checking container states.
// This provides more detailed status than just the pod phase (e.g., CrashLoopBackOff, ImagePullBackOff).
func (ps *PodService) getPodStatus(pod *corev1.Pod) (status string) {
	// Check if pod is being deleted
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
		return status
	}

	// Start with the pod phase
	status = string(pod.Status.Phase)

	// For pods that are "Running" but have container issues, check container states
	// to provide more detailed status information
	if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
		// Check init containers first
		for _, containerStatus := range pod.Status.InitContainerStatuses {
			if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason != "" {
				status = containerStatus.State.Waiting.Reason
				return status
			}
			// Only report terminated init containers if they failed (non-zero exit code)
			// Successfully completed init containers (exitCode 0) are expected behavior
			if containerStatus.State.Terminated != nil && containerStatus.State.Terminated.ExitCode != 0 {
				if containerStatus.State.Terminated.Reason != "" {
					status = containerStatus.State.Terminated.Reason
				} else {
					status = "InitError"
				}
				return status
			}
		}

		// Check regular containers
		for _, containerStatus := range pod.Status.ContainerStatuses {
			// Waiting state (e.g., CrashLoopBackOff, ImagePullBackOff, ContainerCreating)
			if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason != "" {
				status = containerStatus.State.Waiting.Reason
				return status
			}
			// Terminated state - report all terminated containers as they indicate problems
			// (except for successfully completed sidecar/ephemeral containers)
			if containerStatus.State.Terminated != nil {
				// Only report if it's a failure (non-zero exit) or if there's a specific error reason
				// Skip "Completed" status for sidecar containers that exited successfully
				if containerStatus.State.Terminated.ExitCode != 0 {
					if containerStatus.State.Terminated.Reason != "" {
						status = containerStatus.State.Terminated.Reason
					} else {
						status = "Error"
					}
					return status
				}
			}
		}
	}

	return status
}

// extractImageTag extracts the tag from the container images in the pod.
// If there are multiple containers, it returns the tag from the first container.
// If there's no tag specified, it returns "latest".
func extractImageTag(pod *corev1.Pod) string {
	if len(pod.Spec.Containers) == 0 {
		return "unknown"
	}

	image := pod.Spec.Containers[0].Image

	// Handle different image formats:
	// - registry/image:tag
	// - image:tag
	// - image (no tag, implies latest)

	// Find the last colon that's not part of a port number
	lastColon := strings.LastIndex(image, ":")
	if lastColon == -1 {
		return "latest"
	}

	// Check if what comes after the colon looks like a port (registry:port/image)
	afterColon := image[lastColon+1:]
	if strings.Contains(afterColon, "/") {
		// This colon is part of registry:port, so there's no tag
		return "latest"
	}

	// Extract the tag
	tag := strings.TrimSpace(afterColon)
	if tag == "" {
		return "latest"
	}

	return tag
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// matchesRegexSelector checks if pod labels match a regex selector.
// Supports patterns like "app=~nginx.*", "environment=~dev|staging", etc.
// Can handle multiple regex selectors separated by commas.
func (ps *PodService) matchesRegexSelector(labels map[string]string, selector string) bool {
	if labels == nil {
		return false
	}

	// Split by commas to handle multiple selectors
	selectors := strings.Split(selector, ",")

	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}

		// Check if this is a regex selector (contains =~)
		if strings.Contains(sel, "=~") {
			parts := strings.SplitN(sel, "=~", 2)
			if len(parts) != 2 {
				ps.logger.Warn("Invalid regex selector format", zap.String("selector", sel))
				return false
			}

			labelKey := strings.TrimSpace(parts[0])
			regexPattern := strings.TrimSpace(parts[1])

			// Get the label value
			labelValue, exists := labels[labelKey]
			if !exists {
				// Label doesn't exist, so this selector doesn't match
				return false
			}

			// Compile and match the regex
			regex, err := regexp.Compile(regexPattern)
			if err != nil {
				ps.logger.Warn("Invalid regex pattern", zap.String("pattern", regexPattern), zap.Error(err))
				return false
			}

			if !regex.MatchString(labelValue) {
				// This regex selector doesn't match
				return false
			}
		} else {
			// Handle standard label selectors (for mixed selectors)
			// This supports basic equality checks like "app=nginx"
			if strings.Contains(sel, "=") && !strings.Contains(sel, "!=") {
				parts := strings.SplitN(sel, "=", 2)
				if len(parts) == 2 {
					labelKey := strings.TrimSpace(parts[0])
					expectedValue := strings.TrimSpace(parts[1])

					labelValue, exists := labels[labelKey]
					if !exists || labelValue != expectedValue {
						return false
					}
				}
			} else {
				ps.logger.Warn("Unsupported selector format", zap.String("selector", sel))
				return false
			}
		}
	}

	return true
}