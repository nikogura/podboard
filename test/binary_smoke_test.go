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
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBinarySmoke tests that built binaries actually work
func TestBinarySmoke(t *testing.T) {
	// Skip if not in binary testing mode
	if os.Getenv("PODBOARD_BINARY_TEST") != "true" {
		t.Skip("Binary smoke tests require PODBOARD_BINARY_TEST=true")
	}

	releaseDir := os.Getenv("RELEASE_ASSETS_DIR")
	if releaseDir == "" {
		releaseDir = "release-assets"
	}

	// Test all platform binaries
	binaries := []struct {
		name     string
		platform string
		arch     string
	}{
		{"podboard-linux-amd64", "linux", "amd64"},
		{"podboard-linux-arm64", "linux", "arm64"},
		{"podboard-darwin-amd64", "darwin", "amd64"},
		{"podboard-darwin-arm64", "darwin", "arm64"},
		{"podboard-windows-amd64.exe", "windows", "amd64"},
	}

	for _, binary := range binaries {
		t.Run(fmt.Sprintf("Binary_%s", binary.name), func(t *testing.T) {
			binaryPath := filepath.Join(releaseDir, binary.name)

			// Check if binary exists
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				t.Skipf("Binary %s not found, skipping", binaryPath)
			}

			// Test help command (since --version is not implemented)
			t.Run("help_command_basic", func(t *testing.T) {
				cmd := exec.Command(binaryPath, "--help")
				output, err := cmd.Output()

				// For cross-compiled binaries, this might fail on wrong architecture
				if err != nil {
					if strings.Contains(err.Error(), "exec format error") ||
					   strings.Contains(err.Error(), "cannot execute binary file") {
						t.Skipf("Cannot execute %s binary on this architecture", binary.platform)
					}
					t.Fatalf("Help command failed: %v", err)
				}

				outputStr := string(output)
				assert.Contains(t, outputStr, "podboard",
					"Help output should contain 'podboard'")
				assert.Contains(t, outputStr, "Usage:",
					"Help output should contain usage information")
			})

			// Test flags
			t.Run("flags_validation", func(t *testing.T) {
				cmd := exec.Command(binaryPath, "--invalid-flag")
				output, err := cmd.CombinedOutput()

				// Should exit with error for invalid flag
				if err != nil && (strings.Contains(err.Error(), "exec format error") ||
				   strings.Contains(err.Error(), "cannot execute binary file")) {
					t.Skipf("Cannot execute %s binary on this architecture", binary.platform)
				}

				outputStr := string(output)
				// Should show help or error message for invalid flags
				validResponse := strings.Contains(outputStr, "Usage:") ||
					strings.Contains(outputStr, "unknown flag") ||
					strings.Contains(outputStr, "Error:")

				assert.True(t, validResponse, "Should handle invalid flags gracefully")
			})
		})
	}
}

// TestBinaryStartupSmoke tests that the binary can actually start and serve HTTP
func TestBinaryStartupSmoke(t *testing.T) {
	// Skip if not in binary testing mode
	if os.Getenv("PODBOARD_BINARY_TEST") != "true" {
		t.Skip("Binary startup tests require PODBOARD_BINARY_TEST=true")
	}

	releaseDir := os.Getenv("RELEASE_ASSETS_DIR")
	if releaseDir == "" {
		releaseDir = "release-assets"
	}

	// Only test native architecture binary for startup test
	var binaryPath string

	// Detect current platform
	if strings.Contains(os.Getenv("RUNNER_OS"), "Linux") || os.Getenv("RUNNER_OS") == "" {
		binaryPath = filepath.Join(releaseDir, "podboard-linux-amd64")
	} else if strings.Contains(os.Getenv("RUNNER_OS"), "macOS") {
		binaryPath = filepath.Join(releaseDir, "podboard-darwin-amd64")
	} else if strings.Contains(os.Getenv("RUNNER_OS"), "Windows") {
		binaryPath = filepath.Join(releaseDir, "podboard-windows-amd64.exe")
	} else {
		t.Skip("Unknown platform for startup test")
	}

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skipf("Binary %s not found, skipping startup test", binaryPath)
	}

	t.Run("startup_and_health_check", func(t *testing.T) {
		// Start binary in background
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath, "--bind-address", "127.0.0.1:19999")

		// Capture output for debugging
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Start()
		require.NoError(t, err, "Binary should start successfully")

		defer func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}()

		// Wait for server to be ready
		var resp *http.Response
		var lastErr error

		for i := 0; i < 15; i++ { // Try for 15 seconds
			time.Sleep(1 * time.Second)

			resp, lastErr = http.Get("http://127.0.0.1:19999/health")
			if lastErr == nil {
				defer func() { _ = resp.Body.Close() }()
				break
			}
		}

		require.NoError(t, lastErr, "Health endpoint should be reachable")
		require.NotNil(t, resp, "Should get HTTP response")
		assert.Equal(t, http.StatusOK, resp.StatusCode, "Health endpoint should return 200")
	})
}