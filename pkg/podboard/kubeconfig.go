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
	"errors"
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

// ClusterInfo represents a kubectl cluster.
type ClusterInfo struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// KubeConfigService handles kubeconfig operations.
type KubeConfigService struct {
	logger         *zap.Logger
	inCluster      bool
	kubeconfigPath string
}

// NewKubeConfigService creates a new kubeconfig service.
func NewKubeConfigService(logger *zap.Logger) (service *KubeConfigService) {
	service = &KubeConfigService{
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

// IsInCluster returns true if running inside a Kubernetes cluster.
func (kcs *KubeConfigService) IsInCluster() (inCluster bool) {
	inCluster = kcs.inCluster
	return inCluster
}

// GetClusters returns available kubectl clusters.
func (kcs *KubeConfigService) GetClusters() (clusters []ClusterInfo, err error) {
	if kcs.inCluster {
		err = errors.New("clusters not available when running in cluster")
		return clusters, err
	}

	if kcs.kubeconfigPath == "" {
		err = errors.New("kubeconfig path not found")
		return clusters, err
	}

	// Load the kubeconfig file
	var config *clientcmdapi.Config
	config, err = clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		err = fmt.Errorf("failed to load kubeconfig: %w", err)
		return clusters, err
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
	sort.Slice(clusters, func(i, j int) (less bool) {
		if clusters[i].Current {
			less = true
			return less
		}
		if clusters[j].Current {
			less = false
			return less
		}
		less = clusters[i].Name < clusters[j].Name
		return less
	})

	return clusters, err
}

// GetCurrentContext returns the current context name.
func (kcs *KubeConfigService) GetCurrentContext() (currentContext string, err error) {
	if kcs.inCluster {
		err = errors.New("current context not available when running in cluster")
		return currentContext, err
	}

	var config *clientcmdapi.Config
	config, err = clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		err = fmt.Errorf("failed to load kubeconfig: %w", err)
		return currentContext, err
	}

	currentContext = config.CurrentContext
	return currentContext, err
}

// CreateClientForCluster creates a Kubernetes client for the specified cluster.
func (kcs *KubeConfigService) CreateClientForCluster(clusterName string) (client kubernetes.Interface, err error) {
	if kcs.inCluster {
		// When in cluster, ignore cluster name and use in-cluster config
		inClusterConfig, configErr := rest.InClusterConfig()
		if configErr != nil {
			err = fmt.Errorf("failed to get in-cluster config: %w", configErr)
			return client, err
		}
		client, err = kubernetes.NewForConfig(inClusterConfig)
		return client, err
	}

	// Load kubeconfig
	var config *clientcmdapi.Config
	config, err = clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		err = fmt.Errorf("failed to load kubeconfig: %w", err)
		return client, err
	}

	// Verify cluster exists
	cluster, exists := config.Clusters[clusterName]
	if !exists {
		err = fmt.Errorf("cluster %q not found in kubeconfig", clusterName)
		return client, err
	}

	// Find the most commonly used user for this cluster
	var userName string
	userName, err = kcs.findBestUserForCluster(config, clusterName)
	if err != nil {
		err = fmt.Errorf("failed to find suitable user for cluster %q: %w", clusterName, err)
		return client, err
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

	var restConfig *rest.Config
	restConfig, err = clientConfig.ClientConfig()
	if err != nil {
		err = fmt.Errorf("failed to create client config for cluster %q (user %q): %w", clusterName, userName, err)
		return client, err
	}

	client, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		err = fmt.Errorf("failed to create client for cluster %q (user %q): %w", clusterName, userName, err)
		return client, err
	}

	kcs.logger.Info("Created Kubernetes client for cluster", zap.String("cluster", clusterName), zap.String("user", userName))
	return client, err
}

// GetCurrentCluster returns the current cluster name.
func (kcs *KubeConfigService) GetCurrentCluster() (clusterName string, err error) {
	if kcs.inCluster {
		err = errors.New("current cluster not available when running in cluster")
		return clusterName, err
	}

	var config *clientcmdapi.Config
	config, err = clientcmd.LoadFromFile(kcs.kubeconfigPath)
	if err != nil {
		err = fmt.Errorf("failed to load kubeconfig: %w", err)
		return clusterName, err
	}

	if config.CurrentContext != "" {
		if context, exists := config.Contexts[config.CurrentContext]; exists {
			clusterName = context.Cluster
			return clusterName, err
		}
	}

	return clusterName, err
}

// findBestUserForCluster finds the most commonly used user for a given cluster.
func (kcs *KubeConfigService) findBestUserForCluster(config *clientcmdapi.Config, clusterName string) (bestUser string, err error) {
	// Count how many contexts use each user for this cluster
	userCounts := make(map[string]int)

	for _, context := range config.Contexts {
		if context.Cluster == clusterName {
			userCounts[context.AuthInfo]++
		}
	}

	if len(userCounts) == 0 {
		err = fmt.Errorf("no contexts found for cluster %q", clusterName)
		return bestUser, err
	}

	// Find the most commonly used user
	var maxCount int

	for user, count := range userCounts {
		if count > maxCount {
			maxCount = count
			bestUser = user
		}
	}

	// Verify the user exists in AuthInfos
	if _, exists := config.AuthInfos[bestUser]; !exists {
		err = fmt.Errorf("user %q not found in kubeconfig AuthInfos", bestUser)
		return bestUser, err
	}

	return bestUser, err
}
