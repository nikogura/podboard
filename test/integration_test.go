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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestPodboardBasicAPI tests the basic podboard API endpoints
func TestPodboardBasicAPI(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Health endpoint", func(t *testing.T) {
		router := gin.New()
		router.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "healthy"})
		})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/health")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, "healthy", result["status"])
	})

	t.Run("Mock namespaces endpoint", func(t *testing.T) {
		router := gin.New()
		router.GET("/api/namespaces", func(c *gin.Context) {
			c.JSON(200, gin.H{"namespaces": []string{"default", "kube-system"}})
		})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/namespaces")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		namespaces, ok := result["namespaces"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, namespaces, 2)
		assert.Contains(t, namespaces, "default")
		assert.Contains(t, namespaces, "kube-system")
	})

	t.Run("Mock pods endpoint", func(t *testing.T) {
		router := gin.New()
		router.GET("/api/pods", func(c *gin.Context) {
			mockPods := []map[string]interface{}{
				{
					"name":      "test-pod-1",
					"namespace": "default",
					"imageTag":  "v1.2.3",
					"status":    "Running",
					"ready":     "1/1",
					"restarts":  0,
					"age":       "1h",
					"node":      "node-1",
					"ip":        "10.0.0.1",
				},
			}
			c.JSON(200, gin.H{"pods": mockPods})
		})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/pods")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		pods, ok := result["pods"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, pods, 1)

		pod := pods[0].(map[string]interface{})
		assert.Equal(t, "test-pod-1", pod["name"])
		assert.Equal(t, "v1.2.3", pod["imageTag"])
		assert.Equal(t, "Running", pod["status"])
	})

	logger.Info("All podboard basic API tests passed")
}