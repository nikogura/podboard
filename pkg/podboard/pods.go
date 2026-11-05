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
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
)

const (
	imageTagLatest = "latest"
	// allNamespaces is the namespace value the UI sends to request every namespace.
	allNamespaces = "all"
	// inClusterName keys the informer for the cluster podboard itself runs in.
	inClusterName = "in-cluster"
	// regexSelectorOperator marks a label selector that is matched in-process.
	regexSelectorOperator = "=~"
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

// PodService handles pod-related operations. Pod reads come from the
// InformerManager's per-cluster cache; namespace and pod mutations use a
// direct client.
type PodService struct {
	kubeConfigService *KubeConfigService
	informers         *InformerManager
	logger            *zap.Logger
}

// NewPodService creates a new pod service reading pods from informerManager.
func NewPodService(kubeConfigService *KubeConfigService, informerManager *InformerManager, logger *zap.Logger) (service *PodService) {
	service = &PodService{
		kubeConfigService: kubeConfigService,
		informers:         informerManager,
		logger:            logger,
	}
	return service
}

// GetPods retrieves pods from the specified namespace with optional label selector and cluster.
// Supports regex patterns in label selectors using the =~ operator (e.g., "app=~nginx.*").
// Use namespace="all" to retrieve pods from all namespaces.
//
// Reads are served from the cluster's shared informer cache, so polling costs
// no API calls once the cache is warm.
func (ps *PodService) GetPods(ctx context.Context, clusterName, namespace, labelSelector string) (podInfos []PodInfo, err error) {
	resolvedCluster, resolveErr := ps.resolveClusterName(clusterName)
	if resolveErr != nil {
		err = fmt.Errorf("failed to resolve cluster: %w", resolveErr)
		return podInfos, err
	}

	lister, listerErr := ps.informers.PodLister(ctx, resolvedCluster)
	if listerErr != nil {
		ps.logger.Error("Failed to get pod cache", zap.Error(listerErr), zap.String("cluster", resolvedCluster))
		err = fmt.Errorf("failed to get pod cache: %w", listerErr)
		return podInfos, err
	}

	isRegex := strings.Contains(labelSelector, regexSelectorOperator)

	pods, listErr := listCachedPods(lister, namespace, labelSelector, isRegex)
	if listErr != nil {
		err = fmt.Errorf("failed to list pods: %w", listErr)
		return podInfos, err
	}

	for _, pod := range pods {
		if isRegex && !ps.matchesRegexSelector(pod.Labels, labelSelector) {
			continue
		}

		podInfo := ps.podToPodInfo(pod)
		podInfos = append(podInfos, podInfo)
	}

	return podInfos, err
}

// listCachedPods reads pods for the namespace scope from the cache, applying a
// standard label selector server-side-equivalently in memory. Regex selectors
// are matched by the caller, so they list everything. Results are sorted by
// namespace then name so successive polls render in a stable order.
func listCachedPods(lister listerscorev1.PodLister, namespace, labelSelector string, isRegex bool) (pods []*corev1.Pod, err error) {
	selector := labels.Everything()
	if !isRegex && labelSelector != "" {
		selector, err = labels.Parse(labelSelector)
		if err != nil {
			err = fmt.Errorf("invalid label selector %q: %w", labelSelector, err)
			return pods, err
		}
	}

	if namespace == allNamespaces {
		pods, err = lister.List(selector)
	} else {
		pods, err = lister.Pods(namespace).List(selector)
	}
	if err != nil {
		err = fmt.Errorf("reading pod cache: %w", err)
		return pods, err
	}

	sort.Slice(pods, func(i, j int) (less bool) {
		if pods[i].Namespace != pods[j].Namespace {
			less = pods[i].Namespace < pods[j].Namespace
			return less
		}
		less = pods[i].Name < pods[j].Name
		return less
	})

	return pods, err
}

// GetNamespaces retrieves all namespaces for the given cluster.
func (ps *PodService) GetNamespaces(ctx context.Context, clusterName string) (names []string, err error) {
	var client kubernetes.Interface
	client, err = ps.getClient(clusterName)
	if err != nil {
		err = fmt.Errorf("failed to get Kubernetes client: %w", err)
		return names, err
	}

	var namespaces *corev1.NamespaceList
	namespaces, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		ps.logger.Error("Failed to list namespaces", zap.Error(err), zap.String("cluster", clusterName))
		err = fmt.Errorf("failed to list namespaces: %w", err)
		return names, err
	}

	for _, ns := range namespaces.Items {
		names = append(names, ns.Name)
	}

	return names, err
}

// resolveClusterName turns the cluster a request named into the key used for
// clients and informers: the in-cluster placeholder when running in a pod, the
// kubeconfig's current cluster when the request named none, else the name as given.
func (ps *PodService) resolveClusterName(clusterName string) (resolved string, err error) {
	if ps.kubeConfigService.IsInCluster() {
		resolved = inClusterName
		return resolved, err
	}

	if clusterName != "" {
		resolved = clusterName
		return resolved, err
	}

	resolved, err = ps.kubeConfigService.GetCurrentCluster()
	if err != nil {
		err = fmt.Errorf("no cluster specified and failed to get current cluster: %w", err)
		return resolved, err
	}

	return resolved, err
}

// getClient returns a Kubernetes client for the given cluster.
func (ps *PodService) getClient(clusterName string) (client kubernetes.Interface, err error) {
	resolved, resolveErr := ps.resolveClusterName(clusterName)
	if resolveErr != nil {
		err = resolveErr
		return client, err
	}

	client, err = ps.kubeConfigService.CreateClientForCluster(resolved)
	return client, err
}

// DeletePod deletes a pod by name in the specified namespace and cluster.
func (ps *PodService) DeletePod(ctx context.Context, clusterName, namespace, podName string) (err error) {
	client, clientErr := ps.getClient(clusterName)
	if clientErr != nil {
		err = fmt.Errorf("failed to get Kubernetes client: %w", clientErr)
		return err
	}

	err = client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		ps.logger.Error("Failed to delete pod", zap.Error(err), zap.String("cluster", clusterName), zap.String("namespace", namespace), zap.String("pod", podName))
		err = fmt.Errorf("failed to delete pod %s/%s: %w", namespace, podName, err)
		return err
	}

	ps.logger.Info("Pod deleted successfully", zap.String("cluster", clusterName), zap.String("namespace", namespace), zap.String("pod", podName))
	return err
}

func (ps *PodService) podToPodInfo(pod *corev1.Pod) (info PodInfo) {
	// Calculate ready containers
	readyContainers := 0
	totalContainers := len(pod.Spec.Containers)
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			readyContainers++
		}
	}

	// Calculate restart count
	var restarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		restarts += cs.RestartCount
	}

	// Calculate age
	age := time.Since(pod.CreationTimestamp.Time)
	ageStr := formatDuration(age)

	// Get pod status - check container states for more accurate status
	podStatus := ps.getPodStatus(pod)

	// Extract image tag from first container
	imageTag := extractImageTag(pod)

	info = PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		ImageTag:  imageTag,
		Status:    podStatus,
		Ready:     fmt.Sprintf("%d/%d", readyContainers, totalContainers),
		Restarts:  restarts,
		Age:       ageStr,
		Node:      pod.Spec.NodeName,
		IP:        pod.Status.PodIP,
		Labels:    pod.Labels,
	}
	return info
}

// getPodStatus returns the most accurate status for a pod by checking container states.
// This provides more detailed status than just the pod phase (e.g., CrashLoopBackOff, ImagePullBackOff).
func (ps *PodService) getPodStatus(pod *corev1.Pod) (status string) {
	// Check if pod is being deleted.
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
		return status
	}

	// Start with the pod phase.
	status = string(pod.Status.Phase)

	// For pods that are "Running" but have container issues, check container states
	// to provide more detailed status information.
	if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
		return status
	}

	// Check init containers first.
	if reason := initContainerFailureReason(pod.Status.InitContainerStatuses); reason != "" {
		status = reason
		return status
	}

	// Check regular containers.
	if reason := containerFailureReason(pod.Status.ContainerStatuses); reason != "" {
		status = reason
		return status
	}

	return status
}

// initContainerFailureReason returns a non-empty reason if any init container is in a failure state.
func initContainerFailureReason(statuses []corev1.ContainerStatus) (reason string) {
	for _, cs := range statuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			reason = cs.State.Waiting.Reason
			return reason
		}
		// Only report terminated init containers if they failed (non-zero exit code).
		// Successfully completed init containers (exitCode 0) are expected behavior.
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			reason = cs.State.Terminated.Reason
			if reason == "" {
				reason = "InitError"
			}
			return reason
		}
	}
	return reason
}

// containerFailureReason returns a non-empty reason if any container is in a failure state.
func containerFailureReason(statuses []corev1.ContainerStatus) (reason string) {
	for _, cs := range statuses {
		// Waiting state (e.g., CrashLoopBackOff, ImagePullBackOff, ContainerCreating).
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			reason = cs.State.Waiting.Reason
			return reason
		}
		// Terminated state - only report failures (non-zero exit code).
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			reason = cs.State.Terminated.Reason
			if reason == "" {
				reason = "Error"
			}
			return reason
		}
	}
	return reason
}

// extractImageTag extracts the tag from the container images in the pod.
// If there are multiple containers, it returns the tag from the first container.
// If there's no tag specified, it returns "latest".
func extractImageTag(pod *corev1.Pod) (tag string) {
	if len(pod.Spec.Containers) == 0 {
		tag = "unknown"
		return tag
	}

	image := pod.Spec.Containers[0].Image

	// Handle different image formats:
	// - registry/image:tag
	// - image:tag
	// - image (no tag, implies latest)

	// Find the last colon that's not part of a port number
	lastColon := strings.LastIndex(image, ":")
	if lastColon == -1 {
		tag = imageTagLatest
		return tag
	}

	// Check if what comes after the colon looks like a port (registry:port/image)
	afterColon := image[lastColon+1:]
	if strings.Contains(afterColon, "/") {
		// This colon is part of registry:port, so there's no tag
		tag = imageTagLatest
		return tag
	}

	// Extract the tag
	tag = strings.TrimSpace(afterColon)
	if tag == "" {
		tag = imageTagLatest
		return tag
	}

	return tag
}

func formatDuration(d time.Duration) (formatted string) {
	if d < time.Minute {
		formatted = fmt.Sprintf("%ds", int(d.Seconds()))
		return formatted
	}
	if d < time.Hour {
		formatted = fmt.Sprintf("%dm", int(d.Minutes()))
		return formatted
	}
	if d < 24*time.Hour {
		formatted = fmt.Sprintf("%dh", int(d.Hours()))
		return formatted
	}
	formatted = fmt.Sprintf("%dd", int(d.Hours()/24))
	return formatted
}

// matchesRegexSelector checks if pod labels match a regex selector.
// Supports patterns like "app=~nginx.*", "environment=~dev|staging", etc.
// Can handle multiple regex selectors separated by commas.
func (ps *PodService) matchesRegexSelector(labels map[string]string, selector string) (matches bool) {
	if labels == nil {
		return matches
	}

	// Split by commas to handle multiple selectors.
	selectors := strings.Split(selector, ",")

	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}

		if !ps.matchesSingleSelector(labels, sel) {
			return matches
		}
	}

	matches = true
	return matches
}

// matchesSingleSelector evaluates a single label selector against pod labels.
func (ps *PodService) matchesSingleSelector(labels map[string]string, sel string) (matched bool) {
	if strings.Contains(sel, "=~") {
		matched = ps.matchesRegexPart(labels, sel)
		return matched
	}

	matched = ps.matchesEqualityPart(labels, sel)
	return matched
}

// matchesRegexPart evaluates a single regex selector (key=~pattern) against labels.
func (ps *PodService) matchesRegexPart(labels map[string]string, sel string) (matched bool) {
	parts := strings.SplitN(sel, "=~", 2)
	if len(parts) != 2 {
		ps.logger.Warn("Invalid regex selector format", zap.String("selector", sel))
		return matched
	}

	labelKey := strings.TrimSpace(parts[0])
	regexPattern := strings.TrimSpace(parts[1])

	labelValue, exists := labels[labelKey]
	if !exists {
		return matched
	}

	regex, err := regexp.Compile(regexPattern)
	if err != nil {
		ps.logger.Warn("Invalid regex pattern", zap.String("pattern", regexPattern), zap.Error(err))
		return matched
	}

	matched = regex.MatchString(labelValue)
	return matched
}

// matchesEqualityPart evaluates a single equality selector (key=value) against labels.
func (ps *PodService) matchesEqualityPart(labels map[string]string, sel string) (matched bool) {
	if !strings.Contains(sel, "=") || strings.Contains(sel, "!=") {
		ps.logger.Warn("Unsupported selector format", zap.String("selector", sel))
		return matched
	}

	parts := strings.SplitN(sel, "=", 2)
	if len(parts) != 2 {
		return matched
	}

	labelKey := strings.TrimSpace(parts[0])
	expectedValue := strings.TrimSpace(parts[1])

	labelValue, exists := labels[labelKey]
	if !exists || labelValue != expectedValue {
		return matched
	}

	matched = true
	return matched
}
