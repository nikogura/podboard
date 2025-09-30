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
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDockerImage tests the built Docker image
func TestDockerImage(t *testing.T) {
	// Skip if not in Docker testing mode
	if os.Getenv("PODBOARD_DOCKER_TEST") != "true" {
		t.Skip("Docker tests require PODBOARD_DOCKER_TEST=true")
	}

	// Check if docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available, skipping Docker tests")
	}

	imageName := os.Getenv("DOCKER_IMAGE_NAME")
	if imageName == "" {
		imageName = "ghcr.io/nikogura/podboard:latest"
	}

	t.Run("image_exists", func(t *testing.T) {
		// First try to inspect locally
		cmd := exec.Command("docker", "inspect", imageName)
		err := cmd.Run()

		if err != nil {
			// If image doesn't exist locally, try to pull it
			t.Logf("Image not found locally, attempting to pull %s", imageName)
			pullCmd := exec.Command("docker", "pull", imageName)
			pullErr := pullCmd.Run()
			require.NoError(t, pullErr, "Should be able to pull Docker image %s", imageName)

			// Try inspect again after pulling
			cmd = exec.Command("docker", "inspect", imageName)
			err = cmd.Run()
		}

		require.NoError(t, err, "Docker image should exist after pull if needed")
	})

	t.Run("image_metadata", func(t *testing.T) {
		// First try to inspect locally
		cmd := exec.Command("docker", "inspect", "--format", "{{.Config.ExposedPorts}}", imageName)
		output, err := cmd.Output()

		if err != nil {
			// If image doesn't exist locally, try to pull it
			t.Logf("Image not found locally, attempting to pull %s", imageName)
			pullCmd := exec.Command("docker", "pull", imageName)
			pullErr := pullCmd.Run()
			require.NoError(t, pullErr, "Should be able to pull Docker image %s", imageName)

			// Try inspect again after pulling
			cmd = exec.Command("docker", "inspect", "--format", "{{.Config.ExposedPorts}}", imageName)
			output, err = cmd.Output()
		}

		require.NoError(t, err, "Should be able to inspect image after pull if needed")

		outputStr := string(output)
		assert.Contains(t, outputStr, "9999/tcp", "Image should expose port 9999")
	})

	t.Run("container_starts", func(t *testing.T) {
		// Create a unique container name
		containerName := fmt.Sprintf("podboard-test-%d", time.Now().Unix())

		// Ensure image is available locally (docker run will pull if needed, but let's be explicit)
		inspectCmd := exec.Command("docker", "inspect", imageName)
		if inspectCmd.Run() != nil {
			t.Logf("Image not found locally, pulling %s", imageName)
			pullCmd := exec.Command("docker", "pull", imageName)
			pullErr := pullCmd.Run()
			require.NoError(t, pullErr, "Should be able to pull Docker image %s", imageName)
		}

		// Start container
		cmd := exec.Command("docker", "run", "-d", "--name", containerName, "-p", "19998:9999", imageName)
		err := cmd.Run()
		require.NoError(t, err, "Container should start")

		defer func() {
			// Cleanup container
			_ = exec.Command("docker", "stop", containerName).Run()
			_ = exec.Command("docker", "rm", containerName).Run()
		}()

		// Wait for container to be ready and test health endpoint
		var resp *http.Response
		var lastErr error

		for i := 0; i < 20; i++ { // Try for 20 seconds
			time.Sleep(1 * time.Second)

			resp, lastErr = http.Get("http://127.0.0.1:19998/health")
			if lastErr == nil {
				defer func() { _ = resp.Body.Close() }()
				break
			}
		}

		require.NoError(t, lastErr, "Health endpoint should be reachable from container")
		require.NotNil(t, resp, "Should get HTTP response from container")
		assert.Equal(t, http.StatusOK, resp.StatusCode, "Container health endpoint should return 200")
	})

	t.Run("multi_architecture_support", func(t *testing.T) {
		// Test that the image supports multiple architectures
		cmd := exec.Command("docker", "manifest", "inspect", imageName)
		output, err := cmd.Output()

		if err != nil {
			// Manifest might not be available for local images
			t.Skipf("Cannot inspect manifest for %s, skipping multi-arch test", imageName)
		}

		outputStr := string(output)

		// Check for common architectures
		hasAMD64 := strings.Contains(outputStr, "amd64")
		hasARM64 := strings.Contains(outputStr, "arm64")

		if hasAMD64 || hasARM64 {
			t.Logf("Image supports architectures: AMD64=%v, ARM64=%v", hasAMD64, hasARM64)
		} else {
			t.Log("Multi-architecture manifest not available or detected")
		}
	})
}

// TestDockerCompose tests the docker-compose.yml file
func TestDockerCompose(t *testing.T) {
	// Skip if not in Docker testing mode
	if os.Getenv("PODBOARD_DOCKER_TEST") != "true" {
		t.Skip("Docker Compose tests require PODBOARD_DOCKER_TEST=true")
	}

	// Check if docker-compose is available
	if _, err := exec.LookPath("docker-compose"); err != nil {
		if _, err := exec.LookPath("docker"); err != nil {
			t.Skip("Neither docker-compose nor docker compose available")
		}
	}

	t.Run("compose_file_valid", func(t *testing.T) {
		// Test docker-compose config validation
		var cmd *exec.Cmd

		// Try docker compose (new) first, then docker-compose (legacy)
		if _, err := exec.LookPath("docker"); err == nil {
			cmd = exec.Command("docker", "compose", "-f", "../docker-compose.yml", "config")
		} else {
			cmd = exec.Command("docker-compose", "-f", "../docker-compose.yml", "config")
		}

		err := cmd.Run()
		require.NoError(t, err, "docker-compose.yml should be valid")
	})

	t.Run("compose_startup", func(t *testing.T) {
		// This test would require a kubeconfig to be present, so we'll just validate
		// that the compose file can be parsed and the service definition is correct
		t.Run("service_definition", func(t *testing.T) {
			var cmd *exec.Cmd

			if _, err := exec.LookPath("docker"); err == nil {
				cmd = exec.Command("docker", "compose", "-f", "../docker-compose.yml", "config", "--services")
			} else {
				cmd = exec.Command("docker-compose", "-f", "../docker-compose.yml", "config", "--services")
			}

			output, err := cmd.Output()
			require.NoError(t, err, "Should be able to list services")

			outputStr := string(output)
			assert.Contains(t, outputStr, "podboard", "Should define podboard service")
		})
	})
}