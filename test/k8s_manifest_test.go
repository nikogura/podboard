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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// getK8sDir returns the path to the k8s directory relative to the project root
func getK8sDir() string {
	// Get the directory of the current test file
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)

	// Go up one level to project root and then to k8s
	return filepath.Join(testDir, "..", "k8s")
}

// validateYAMLFile validates that a file contains valid YAML and basic Kubernetes resource structure
func validateYAMLFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Split by document separator for multi-document YAML
	documents := strings.Split(string(content), "---")

	for i, doc := range documents {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue // Skip empty documents
		}

		// Skip documents that are entirely commented out
		lines := strings.Split(doc, "\n")
		hasNonCommentLine := false
		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") {
				hasNonCommentLine = true
				break
			}
		}
		if !hasNonCommentLine {
			continue // Skip documents that are entirely comments
		}

		var yamlDoc map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &yamlDoc); err != nil {
			return fmt.Errorf("invalid YAML in document %d: %w", i+1, err)
		}

		// Basic Kubernetes resource validation
		if kind, exists := yamlDoc["kind"]; !exists {
			return fmt.Errorf("document %d missing required 'kind' field", i+1)
		} else if kindStr, ok := kind.(string); !ok || kindStr == "" {
			return fmt.Errorf("document %d 'kind' field must be a non-empty string", i+1)
		}

		if apiVersion, exists := yamlDoc["apiVersion"]; !exists {
			return fmt.Errorf("document %d missing required 'apiVersion' field", i+1)
		} else if apiVersionStr, ok := apiVersion.(string); !ok || apiVersionStr == "" {
			return fmt.Errorf("document %d 'apiVersion' field must be a non-empty string", i+1)
		}

		if metadata, exists := yamlDoc["metadata"]; !exists {
			return fmt.Errorf("document %d missing required 'metadata' field", i+1)
		} else if metadataMap, ok := metadata.(map[string]interface{}); !ok {
			return fmt.Errorf("document %d 'metadata' field must be an object", i+1)
		} else if name, exists := metadataMap["name"]; !exists {
			return fmt.Errorf("document %d metadata missing required 'name' field", i+1)
		} else if nameStr, ok := name.(string); !ok || nameStr == "" {
			return fmt.Errorf("document %d metadata 'name' field must be a non-empty string", i+1)
		}
	}

	return nil
}

// TestKubernetesManifests tests that Kubernetes manifests are valid
func TestKubernetesManifests(t *testing.T) {
	// Skip if not in K8s testing mode
	if os.Getenv("PODBOARD_K8S_TEST") != "true" {
		t.Skip("Kubernetes manifest tests require PODBOARD_K8S_TEST=true")
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

			// Validate YAML syntax only (no Kubernetes API server required)
			// Use yq or built-in YAML validation instead of kubectl
			err := validateYAMLFile(manifestPath)
			require.NoError(t, err, "Manifest %s should be valid YAML", manifest)

			t.Logf("Manifest %s validated successfully", manifest)
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

	// Validate YAML syntax and structure
	err = validateYAMLFile(manifestPath)
	require.NoError(t, err, "All-in-one manifest should be valid YAML")
}