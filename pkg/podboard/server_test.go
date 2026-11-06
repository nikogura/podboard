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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	deleteRoutePath = "/api/pods/:namespace/:name"
	stubClusterName = "stub"
)

// apiServerStub stands in for a cluster API server and records every request
// podboard sends it, so a test can prove whether a delete reached the cluster.
type apiServerStub struct {
	mu       sync.Mutex
	requests []string
	server   *httptest.Server
}

func newAPIServerStub(t *testing.T) (stub *apiServerStub) {
	t.Helper()

	stub = &apiServerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.requests = append(stub.requests, r.Method+" "+r.URL.Path)
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Success"}`))
	}))
	t.Cleanup(stub.server.Close)

	return stub
}

// seen returns the requests the stub has received, as "METHOD /path".
func (s *apiServerStub) seen() (requests []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	requests = append(requests, s.requests...)
	return requests
}

// kubeconfigFor writes a kubeconfig whose only cluster is the stub.
func kubeconfigFor(t *testing.T, stub *apiServerStub) (path string) {
	t.Helper()

	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %[1]s
  cluster:
    server: %[2]s
contexts:
- name: %[1]s
  context:
    cluster: %[1]s
    user: %[1]s
current-context: %[1]s
users:
- name: %[1]s
  user:
    token: stub-token
`, stubClusterName, stub.server.URL)

	path = writeKubeconfig(t, "config", content)
	return path
}

// newTestRouter wires the API the way RunServer does, against a kubeconfig
// that points at stub.
func newTestRouter(t *testing.T, stub *apiServerStub, readOnly bool) (router *gin.Engine) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	env := envOf(map[string]string{EnvKubeconfig: kubeconfigFor(t, stub)})
	kubeConfigService := newKubeConfigService(zap.NewNop(), env, inClusterProbe(false))

	manager := NewInformerManager(kubeConfigService.CreateClientForCluster, DefaultInformerConfig(), zap.NewNop())
	t.Cleanup(manager.Stop)

	podService := NewPodService(kubeConfigService, manager, zap.NewNop())

	router = gin.New()
	setupAPIRoutes(router, podService, kubeConfigService, readOnly)

	return router
}

// routeMethods returns the HTTP methods registered for path.
func routeMethods(router *gin.Engine, path string) (methods []string) {
	for _, route := range router.Routes() {
		if route.Path == path {
			methods = append(methods, route.Method)
		}
	}
	return methods
}

// do performs req against router and decodes a JSON object body if there is one.
func do(t *testing.T, router *gin.Engine, method, target string) (status int, body map[string]any) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	status = rec.Code
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}

	return status, body
}

func TestResolveReadOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flag    bool
		env     string
		want    bool
		wantErr string
	}{
		{name: "deletion enabled by default", flag: false, env: "", want: false},
		{name: "flag turns read-only on", flag: true, env: "", want: true},
		{name: "env true turns read-only on", flag: false, env: "true", want: true},
		{name: "env 1 turns read-only on", flag: false, env: "1", want: true},
		{name: "env false leaves deletion enabled", flag: false, env: "false", want: false},
		{name: "env false cannot override the flag", flag: true, env: "false", want: true},
		{name: "env garbage is an error", flag: false, env: "sometimes", wantErr: EnvReadOnly + `="sometimes" is not a boolean`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveReadOnly(tt.flag, envOf(map[string]string{EnvReadOnly: tt.env}))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfigEndpointReportsReadOnly(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%t", readOnly), func(t *testing.T) {
			router := newTestRouter(t, newAPIServerStub(t), readOnly)

			status, body := do(t, router, http.MethodGet, "/api/config")

			require.Equal(t, http.StatusOK, status)
			assert.Equal(t, readOnly, body["readOnly"])
		})
	}
}

func TestDeleteRouteIsServedByDefault(t *testing.T) {
	stub := newAPIServerStub(t)
	router := newTestRouter(t, stub, false)

	assert.Equal(t, []string{http.MethodDelete}, routeMethods(router, deleteRoutePath))

	status, body := do(t, router, http.MethodDelete, "/api/pods/default/web?cluster="+stubClusterName)

	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "Pod deleted successfully", body["message"])
	assert.Equal(t, []string{"DELETE /api/v1/namespaces/default/pods/web"}, stub.seen())
}

func TestReadOnlyModeHasNoDeleteRoute(t *testing.T) {
	stub := newAPIServerStub(t)
	router := newTestRouter(t, stub, true)

	assert.Empty(t, routeMethods(router, deleteRoutePath), "read-only mode must not register the delete route")

	status, _ := do(t, router, http.MethodDelete, "/api/pods/default/web?cluster="+stubClusterName)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Empty(t, stub.seen(), "a delete in read-only mode must never reach the cluster")

	// Viewing still works.
	status, body := do(t, router, http.MethodGet, "/api/clusters")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, false, body["inCluster"])
	assert.Len(t, body["clusters"], 1)
}
