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

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getK8sDir returns the path to the k8s directory relative to the project root
func getK8sDir() string {
	// Get the directory of the current test file
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)

	// Go up one level to project root and then to k8s
	return filepath.Join(testDir, "..", "k8s")
}

// TestKubernetesManifests tests that Kubernetes manifests are valid
func TestKubernetesManifests(t *testing.T) {
	// Skip if not in K8s testing mode
	if os.Getenv("PODBOARD_K8S_TEST") != "true" {
		t.Skip("Kubernetes manifest tests require PODBOARD_K8S_TEST=true")
	}

	// Check if kubectl is available
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not available, skipping Kubernetes manifest tests")
	}

	k8sDir := getK8sDir()
	manifests := []string{
		"rbac-namespace-restricted.yaml",
		"rbac-cluster-wide.yaml",
		"deployment.yaml",
		"service.yaml",
		"all-in-one-namespace-restricted.yaml",
	}

	for _, manifest := range manifests {
		t.Run(filepath.Base(manifest), func(t *testing.T) {
			manifestPath := filepath.Join(k8sDir, manifest)

			// Check if manifest file exists
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				t.Skipf("Manifest %s not found, skipping", manifestPath)
			}

			// Validate YAML syntax with kubectl (client-side only, no server required)
			cmd := exec.Command("kubectl", "apply", "--dry-run=client", "--validate=false", "-f", manifestPath)
			output, err := cmd.CombinedOutput()

			require.NoError(t, err, "Manifest %s should be valid YAML: %s", manifest, string(output))

			outputStr := string(output)
			if strings.Contains(outputStr, "created") || strings.Contains(outputStr, "configured") {
				t.Logf("Manifest %s validated successfully", manifest)
			}
		})
	}
}

// TestRBACPermissions tests that RBAC configurations have expected permissions
func TestRBACPermissions(t *testing.T) {
	// Skip if not in K8s testing mode
	if os.Getenv("PODBOARD_K8S_TEST") != "true" {
		t.Skip("RBAC permission tests require PODBOARD_K8S_TEST=true")
	}

	testCases := []struct {
		name         string
		manifestPath string
		expectations []string
	}{
		{
			name:         "namespace-restricted",
			manifestPath: filepath.Join(getK8sDir(), "rbac-namespace-restricted.yaml"),
			expectations: []string{
				"ServiceAccount",
				"Role",
				"ClusterRole",
				"RoleBinding",
				"ClusterRoleBinding",
				"podboard-namespace",
				"podboard-cluster-readonly",
			},
		},
		{
			name:         "cluster-wide",
			manifestPath: filepath.Join(getK8sDir(), "rbac-cluster-wide.yaml"),
			expectations: []string{
				"ServiceAccount",
				"ClusterRole",
				"ClusterRoleBinding",
				"podboard-cluster-admin",
				"security-risk",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Check if manifest file exists
			if _, err := os.Stat(tc.manifestPath); os.IsNotExist(err) {
				t.Skipf("Manifest %s not found, skipping", tc.manifestPath)
			}

			// Read manifest content
			content, err := os.ReadFile(tc.manifestPath)
			require.NoError(t, err, "Should be able to read manifest")

			contentStr := string(content)

			// Check all expectations are present
			for _, expectation := range tc.expectations {
				assert.Contains(t, contentStr, expectation,
					"Manifest %s should contain %s", tc.manifestPath, expectation)
			}

			// Additional checks for cluster-wide RBAC
			if tc.name == "cluster-wide" {
				assert.Contains(t, contentStr, "WARNING",
					"Cluster-wide RBAC should contain warnings")
				assert.Contains(t, contentStr, "DANGEROUS",
					"Cluster-wide RBAC should be marked as dangerous")
			}
		})
	}
}

// TestDeploymentManifest tests deployment-specific configurations
func TestDeploymentManifest(t *testing.T) {
	// Skip if not in K8s testing mode
	if os.Getenv("PODBOARD_K8S_TEST") != "true" {
		t.Skip("Deployment manifest tests require PODBOARD_K8S_TEST=true")
	}

	manifestPath := filepath.Join(getK8sDir(), "deployment.yaml")

	// Check if manifest file exists
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skipf("Deployment manifest not found, skipping")
	}

	// Read manifest content
	content, err := os.ReadFile(manifestPath)
	require.NoError(t, err, "Should be able to read deployment manifest")

	contentStr := string(content)

	t.Run("security_context", func(t *testing.T) {
		assert.Contains(t, contentStr, "securityContext",
			"Deployment should include security context")
		assert.Contains(t, contentStr, "allowPrivilegeEscalation: false",
			"Deployment should disable privilege escalation")
		assert.Contains(t, contentStr, "readOnlyRootFilesystem: true",
			"Deployment should use read-only root filesystem")
		assert.Contains(t, contentStr, "runAsNonRoot: true",
			"Deployment should run as non-root user")
	})

	t.Run("resource_limits", func(t *testing.T) {
		assert.Contains(t, contentStr, "resources:",
			"Deployment should include resource limits")
		assert.Contains(t, contentStr, "limits:",
			"Deployment should include resource limits")
		assert.Contains(t, contentStr, "requests:",
			"Deployment should include resource requests")
	})

	t.Run("health_checks", func(t *testing.T) {
		assert.Contains(t, contentStr, "livenessProbe",
			"Deployment should include liveness probe")
		assert.Contains(t, contentStr, "readinessProbe",
			"Deployment should include readiness probe")
		assert.Contains(t, contentStr, "/health",
			"Health checks should use /health endpoint")
	})

	t.Run("service_account", func(t *testing.T) {
		assert.Contains(t, contentStr, "serviceAccountName: podboard",
			"Deployment should use podboard service account")
	})
}

// TestAllInOneManifest tests the all-in-one deployment
func TestAllInOneManifest(t *testing.T) {
	// Skip if not in K8s testing mode
	if os.Getenv("PODBOARD_K8S_TEST") != "true" {
		t.Skip("All-in-one manifest tests require PODBOARD_K8S_TEST=true")
	}

	manifestPath := filepath.Join(getK8sDir(), "all-in-one-namespace-restricted.yaml")

	// Check if manifest file exists
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skipf("All-in-one manifest not found, skipping")
	}

	// Read manifest content
	content, err := os.ReadFile(manifestPath)
	require.NoError(t, err, "Should be able to read all-in-one manifest")

	contentStr := string(content)

	// Check that all necessary resources are included
	expectedResources := []string{
		"ServiceAccount",
		"Role",
		"ClusterRole",
		"RoleBinding",
		"ClusterRoleBinding",
		"Deployment",
		"Service",
	}

	for _, resource := range expectedResources {
		assert.Contains(t, contentStr, fmt.Sprintf("kind: %s", resource),
			"All-in-one manifest should contain %s", resource)
	}

	// Validate with kubectl (client-side only, no server required)
	if _, err := exec.LookPath("kubectl"); err == nil {
		cmd := exec.Command("kubectl", "apply", "--dry-run=client", "--validate=false", "-f", manifestPath)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "All-in-one manifest should be valid: %s", string(output))
	}
}