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
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ClusterInfo represents a kubectl cluster
type ClusterInfo struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// KubeConfigService handles kubeconfig operations
type KubeConfigService struct {
	logger      *zap.Logger
	inCluster   bool
	kubeconfigPath string
}

// NewKubeConfigService creates a new kubeconfig service
func NewKubeConfigService(logger *zap.Logger) *KubeConfigService {
	service := &KubeConfigService{
		logger: logger,
	}

	// Check if running in cluster
	_, err := rest.InClusterConfig()
	service.inCluster = err == nil

	if !service.inCluster {
		// Determine kubeconfig path
		service.kubeconfigPath = os.Getenv("KUBECONFIG")
		if service.kubeconfigPath == "" {
			if home := homeDir(); home != "" {
				service.kubeconfigPath = filepath.Join(home, ".kube", "config")
			}
		}
	}

	return service
}

// IsInCluster returns true if running inside a Kubernetes cluster
func (kcs *KubeConfigService) IsInCluster() bool {
	return kcs.inCluster
}

// GetClusters returns available kubectl clusters
func (kcs *KubeConfigService) GetClusters() ([]ClusterInfo, error) {
	if kcs.inCluster {
		return nil, fmt.Errorf("clusters not available when running in cluster")
	}

	if kcs.kubeconfigPath == "" {
		return nil, fmt.Errorf("kubeconfig path not found")
	}

	// Load the kubeconfig file
	config, err := clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Get current cluster from current context
	var currentCluster string
	if config.CurrentContext != "" {
		if context, exists := config.Contexts[config.CurrentContext]; exists {
			currentCluster = context.Cluster
		}
	}

	// Extract unique clusters
	clusterMap := make(map[string]bool)
	var clusters []ClusterInfo

	for clusterName := range config.Clusters {
		if !clusterMap[clusterName] {
			clusterMap[clusterName] = true
			clusterInfo := ClusterInfo{
				Name:    clusterName,
				Current: clusterName == currentCluster,
			}
			clusters = append(clusters, clusterInfo)
		}
	}

	// Sort clusters alphabetically, but put current cluster first
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Current {
			return true
		}
		if clusters[j].Current {
			return false
		}
		return clusters[i].Name < clusters[j].Name
	})

	return clusters, nil
}

// GetCurrentContext returns the current context name
func (kcs *KubeConfigService) GetCurrentContext() (string, error) {
	if kcs.inCluster {
		return "", fmt.Errorf("current context not available when running in cluster")
	}

	config, err := clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	return config.CurrentContext, nil
}

// CreateClientForCluster creates a Kubernetes client for the specified cluster
func (kcs *KubeConfigService) CreateClientForCluster(clusterName string) (kubernetes.Interface, error) {
	if kcs.inCluster {
		// When in cluster, ignore cluster name and use in-cluster config
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
		return kubernetes.NewForConfig(config)
	}

	// Load kubeconfig
	config, err := clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Verify cluster exists
	cluster, exists := config.Clusters[clusterName]
	if !exists {
		return nil, fmt.Errorf("cluster %q not found in kubeconfig", clusterName)
	}

	// Find the most commonly used user for this cluster
	userName, err := kcs.findBestUserForCluster(config, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to find suitable user for cluster %q: %w", clusterName, err)
	}

	// Create a virtual context combining the cluster and user
	virtualContext := &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: userName,
	}

	// Create client config with virtual context
	clientConfig := clientcmd.NewDefaultClientConfig(clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{clusterName: cluster},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{userName: config.AuthInfos[userName]},
		Contexts:  map[string]*clientcmdapi.Context{"virtual": virtualContext},
	}, &clientcmd.ConfigOverrides{
		CurrentContext: "virtual",
	})

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create client config for cluster %q (user %q): %w", clusterName, userName, err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for cluster %q (user %q): %w", clusterName, userName, err)
	}

	kcs.logger.Info("Created Kubernetes client for cluster", zap.String("cluster", clusterName), zap.String("user", userName))
	return client, nil
}

// GetCurrentCluster returns the current cluster name
func (kcs *KubeConfigService) GetCurrentCluster() (string, error) {
	if kcs.inCluster {
		return "", fmt.Errorf("current cluster not available when running in cluster")
	}

	config, err := clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	if config.CurrentContext != "" {
		if context, exists := config.Contexts[config.CurrentContext]; exists {
			return context.Cluster, nil
		}
	}

	return "", nil
}

// findBestUserForCluster finds the most commonly used user for a given cluster
func (kcs *KubeConfigService) findBestUserForCluster(config *clientcmdapi.Config, clusterName string) (string, error) {
	// Count how many contexts use each user for this cluster
	userCounts := make(map[string]int)

	for _, context := range config.Contexts {
		if context.Cluster == clusterName {
			userCounts[context.AuthInfo]++
		}
	}

	if len(userCounts) == 0 {
		return "", fmt.Errorf("no contexts found for cluster %q", clusterName)
	}

	// Find the most commonly used user
	var bestUser string
	var maxCount int

	for user, count := range userCounts {
		if count > maxCount {
			maxCount = count
			bestUser = user
		}
	}

	// Verify the user exists in AuthInfos
	if _, exists := config.AuthInfos[bestUser]; !exists {
		return "", fmt.Errorf("user %q not found in kubeconfig AuthInfos", bestUser)
	}

	return bestUser, nil
}