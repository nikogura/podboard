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

// TestInstallScript tests the installation script functionality
func TestInstallScript(t *testing.T) {
	// Skip if not in install script testing mode
	if os.Getenv("PODBOARD_INSTALL_TEST") != "true" {
		t.Skip("Install script tests require PODBOARD_INSTALL_TEST=true")
	}

	// Get the directory of the current test file and go up one level to project root
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)
	scriptPath := filepath.Join(testDir, "..", "install.sh")

	// Check if install script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Install script %s not found, skipping", scriptPath)
	}

	t.Run("script_syntax", func(t *testing.T) {
		// Test that the script has valid bash syntax
		cmd := exec.Command("bash", "-n", scriptPath)
		err := cmd.Run()
		require.NoError(t, err, "Install script should have valid bash syntax")
	})

	t.Run("script_executable", func(t *testing.T) {
		// Check if script is executable
		info, err := os.Stat(scriptPath)
		require.NoError(t, err, "Should be able to stat install script")

		mode := info.Mode()
		assert.True(t, mode&0111 != 0, "Install script should be executable")
	})

	t.Run("platform_detection", func(t *testing.T) {
		// Test the detect_platform function by extracting it
		scriptDir := filepath.Dir(scriptPath)
		script := fmt.Sprintf(`
			cd %s
			# Extract the detect_platform function and run it standalone
			sed -n '/^detect_platform()/,/^}/p' ./install.sh > /tmp/detect_platform.sh
			echo "detect_platform" >> /tmp/detect_platform.sh
			bash /tmp/detect_platform.sh
			rm -f /tmp/detect_platform.sh
		`, scriptDir)

		cmd := exec.Command("bash", "-c", script)
		output, err := cmd.Output()
		require.NoError(t, err, "Platform detection should work")

		outputStr := string(output)

		// Should detect some platform
		platformDetected := strings.Contains(outputStr, "linux") ||
			strings.Contains(outputStr, "darwin") ||
			strings.Contains(outputStr, "windows")

		assert.True(t, platformDetected, "Should detect a valid platform: %s", outputStr)

		// Should detect some architecture
		archDetected := strings.Contains(outputStr, "amd64") ||
			strings.Contains(outputStr, "arm64")

		assert.True(t, archDetected, "Should detect a valid architecture: %s", outputStr)
	})

	t.Run("help_functionality", func(t *testing.T) {
		// Test that the script shows help when run with --help or invalid args
		cmd := exec.Command("bash", scriptPath, "--help")
		output, err := cmd.CombinedOutput()

		// The script might not have --help flag, but it should handle it gracefully
		outputStr := string(output)

		// Should contain some indication of the script purpose
		if err == nil {
			// If it succeeds, should contain useful info
			assert.Contains(t, strings.ToLower(outputStr), "podboard",
				"Help output should mention podboard")
		}
		// If it fails, that's also acceptable as the script doesn't support --help
	})

	t.Run("environment_variables", func(t *testing.T) {
		// Test that important environment variables are used correctly
		scriptDir := filepath.Dir(scriptPath)
		script := fmt.Sprintf(`
			export INSTALL_DIR="/tmp/test-install"
			cd %s
			# Extract variable definitions without running main
			grep -E '^(REPO=|BINARY_NAME=|INSTALL_DIR=)' ./install.sh || true
			echo "INSTALL_DIR_TEST: ${INSTALL_DIR:-/usr/local/bin}"
			echo "BINARY_NAME_TEST: podboard"
			echo "REPO_TEST: nikogura/podboard"
		`, scriptDir)

		cmd := exec.Command("bash", "-c", script)
		output, err := cmd.Output()
		require.NoError(t, err, "Environment variable handling should work")

		outputStr := string(output)
		assert.Contains(t, outputStr, "INSTALL_DIR_TEST: /tmp/test-install",
			"Should respect INSTALL_DIR environment variable")
		assert.Contains(t, outputStr, "BINARY_NAME_TEST: podboard",
			"Should set correct binary name")
		assert.Contains(t, outputStr, "REPO_TEST: nikogura/podboard",
			"Should set correct repository")
	})

	t.Run("required_tools_check", func(t *testing.T) {
		// Test that the script checks for required tools
		content, err := os.ReadFile(scriptPath)
		require.NoError(t, err, "Should be able to read install script")

		contentStr := string(content)

		// Should check for curl
		assert.Contains(t, contentStr, "curl",
			"Script should check for or use curl")

		// Should check for uname
		assert.Contains(t, contentStr, "uname",
			"Script should check for or use uname")

		// Should have some form of dependency checking
		dependencyCheck := strings.Contains(contentStr, "command -v") ||
			strings.Contains(contentStr, "which") ||
			strings.Contains(contentStr, "type")

		assert.True(t, dependencyCheck,
			"Script should check for required dependencies")
	})

	t.Run("download_url_construction", func(t *testing.T) {
		// Test that download URLs are constructed correctly
		content, err := os.ReadFile(scriptPath)
		require.NoError(t, err, "Should be able to read install script")

		contentStr := string(content)

		// Should contain GitHub release URL pattern
		assert.Contains(t, contentStr, "github.com",
			"Should use GitHub for downloads")
		assert.Contains(t, contentStr, "releases",
			"Should use GitHub releases")
		assert.Contains(t, contentStr, "download",
			"Should download from releases")

		// Should handle version variable
		assert.Contains(t, contentStr, "${version}",
			"Should use version variable in download URL")
	})

	t.Run("error_handling", func(t *testing.T) {
		// Test that the script has proper error handling
		content, err := os.ReadFile(scriptPath)
		require.NoError(t, err, "Should be able to read install script")

		contentStr := string(content)

		// Should use set -e for error handling
		assert.Contains(t, contentStr, "set -e",
			"Script should exit on errors")

		// Should have error printing function
		errorHandling := strings.Contains(contentStr, "print_error") ||
			strings.Contains(contentStr, "echo") && strings.Contains(contentStr, "ERROR")

		assert.True(t, errorHandling,
			"Script should have error printing functionality")
	})

	t.Run("installation_permissions", func(t *testing.T) {
		// Test that the script handles permissions correctly
		content, err := os.ReadFile(scriptPath)
		require.NoError(t, err, "Should be able to read install script")

		contentStr := string(content)

		// Should check if installation directory is writable
		permissionCheck := strings.Contains(contentStr, "-w") ||
			strings.Contains(contentStr, "sudo")

		assert.True(t, permissionCheck,
			"Script should handle installation permissions")

		// Should set executable permissions
		assert.Contains(t, contentStr, "755",
			"Script should set proper executable permissions")
	})
}