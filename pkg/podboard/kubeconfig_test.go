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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const hubAndSpokeKubeconfig = `apiVersion: v1
kind: Config
current-context: hub
clusters:
- name: hub
  cluster:
    server: https://kubernetes.default.svc
    insecure-skip-tls-verify: true
- name: spoke-a
  cluster:
    server: https://spoke-a.example.com:6443
    insecure-skip-tls-verify: true
contexts:
- name: hub
  context:
    cluster: hub
    user: hub-sa
- name: spoke-a
  context:
    cluster: spoke-a
    user: spoke-a-ro
users:
- name: hub-sa
  user:
    token: hub-token
- name: spoke-a-ro
  user:
    token: spoke-token
`

const spokeBKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: spoke-b
  cluster:
    server: https://spoke-b.example.com:6443
    insecure-skip-tls-verify: true
contexts:
- name: spoke-b
  context:
    cluster: spoke-b
    user: spoke-b-ro
users:
- name: spoke-b-ro
  user:
    token: spoke-b-token
`

// envOf returns a getenv backed by the map, with every other key unset.
func envOf(values map[string]string) (getenv func(key string) (value string)) {
	getenv = func(key string) (value string) {
		value = values[key]
		return value
	}
	return getenv
}

// inClusterProbe returns an in-cluster detector with a fixed answer.
func inClusterProbe(answer bool) (probe func() (available bool)) {
	probe = func() (available bool) {
		available = answer
		return available
	}
	return probe
}

// writeKubeconfig writes content to a file in a fresh temp dir and returns its path.
func writeKubeconfig(t *testing.T, name, content string) (path string) {
	t.Helper()

	path = filepath.Join(t.TempDir(), name)
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)

	return path
}

// clusterNames flattens ClusterInfo to names in the order returned.
func clusterNames(clusters []ClusterInfo) (names []string) {
	names = make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		names = append(names, cluster.Name)
	}
	return names
}

func TestNewKubeConfigServicePrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		env           map[string]string
		inCluster     bool
		wantInCluster bool
		wantPath      string
	}{
		{
			name:          "explicit KUBECONFIG wins inside a pod",
			env:           map[string]string{EnvKubeconfig: "/etc/podboard/kubeconfig", "HOME": "/home/podboard"},
			inCluster:     true,
			wantInCluster: false,
			wantPath:      "/etc/podboard/kubeconfig",
		},
		{
			name:          "explicit KUBECONFIG outside a pod",
			env:           map[string]string{EnvKubeconfig: "/tmp/kc", "HOME": "/home/me"},
			wantInCluster: false,
			wantPath:      "/tmp/kc",
		},
		{
			name:          "explicit KUBECONFIG path list is kept whole",
			env:           map[string]string{EnvKubeconfig: "/a" + string(os.PathListSeparator) + "/b"},
			inCluster:     true,
			wantInCluster: false,
			wantPath:      "/a" + string(os.PathListSeparator) + "/b",
		},
		{
			name:          "in-cluster when KUBECONFIG is unset",
			env:           map[string]string{"HOME": "/home/podboard"},
			inCluster:     true,
			wantInCluster: true,
			wantPath:      "",
		},
		{
			name:          "home default outside a pod",
			env:           map[string]string{"HOME": "/home/me"},
			wantInCluster: false,
			wantPath:      filepath.Join("/home/me", ".kube", "config"),
		},
		{
			name:          "windows home outside a pod",
			env:           map[string]string{"USERPROFILE": `C:\Users\me`},
			wantInCluster: false,
			wantPath:      filepath.Join(`C:\Users\me`, ".kube", "config"),
		},
		{
			name:          "no home and no pod leaves the path empty",
			env:           map[string]string{},
			wantInCluster: false,
			wantPath:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := newKubeConfigService(zap.NewNop(), envOf(tt.env), inClusterProbe(tt.inCluster))

			assert.Equal(t, tt.wantInCluster, service.IsInCluster())
			assert.Equal(t, tt.wantPath, service.kubeconfigPath)
		})
	}
}

func TestGetClustersFromExplicitKubeconfigInsideAPod(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, "kubeconfig", hubAndSpokeKubeconfig)
	service := newKubeConfigService(zap.NewNop(), envOf(map[string]string{EnvKubeconfig: path}), inClusterProbe(true))

	require.False(t, service.IsInCluster())

	clusters, err := service.GetClusters()
	require.NoError(t, err)
	assert.Equal(t, []string{"hub", "spoke-a"}, clusterNames(clusters), "current cluster sorts first")
	assert.True(t, clusters[0].Current)
	assert.False(t, clusters[1].Current)

	current, err := service.GetCurrentCluster()
	require.NoError(t, err)
	assert.Equal(t, "hub", current)

	currentContext, err := service.GetCurrentContext()
	require.NoError(t, err)
	assert.Equal(t, "hub", currentContext)
}

func TestGetClustersMergesKubeconfigPathList(t *testing.T) {
	t.Parallel()

	first := writeKubeconfig(t, "hub", hubAndSpokeKubeconfig)
	second := writeKubeconfig(t, "spoke-b", spokeBKubeconfig)
	list := first + string(os.PathListSeparator) + second
	service := newKubeConfigService(zap.NewNop(), envOf(map[string]string{EnvKubeconfig: list}), inClusterProbe(true))

	clusters, err := service.GetClusters()
	require.NoError(t, err)
	assert.Equal(t, []string{"hub", "spoke-a", "spoke-b"}, clusterNames(clusters))

	_, err = service.CreateClientForCluster("spoke-b")
	require.NoError(t, err, "clusters from later files in the list must be usable")
}

func TestCreateClientForClusterFromExplicitKubeconfig(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, "kubeconfig", hubAndSpokeKubeconfig)
	service := newKubeConfigService(zap.NewNop(), envOf(map[string]string{EnvKubeconfig: path}), inClusterProbe(true))

	for _, name := range []string{"hub", "spoke-a"} {
		client, err := service.CreateClientForCluster(name)
		require.NoError(t, err, name)
		assert.NotNil(t, client)
	}

	_, err := service.CreateClientForCluster("nope")
	require.ErrorContains(t, err, `cluster "nope" not found in kubeconfig`)
}

func TestInClusterModeStillHidesTheClusterList(t *testing.T) {
	t.Parallel()

	service := newKubeConfigService(zap.NewNop(), envOf(map[string]string{}), inClusterProbe(true))

	require.True(t, service.IsInCluster())

	_, err := service.GetClusters()
	require.ErrorContains(t, err, "clusters not available when running in cluster")
}

func TestExplicitKubeconfigThatDoesNotExistIsAnError(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent")
	service := newKubeConfigService(zap.NewNop(), envOf(map[string]string{EnvKubeconfig: missing}), inClusterProbe(true))

	_, err := service.GetClusters()
	require.ErrorContains(t, err, "failed to load kubeconfig")
	require.ErrorContains(t, err, missing)

	_, err = service.CreateClientForCluster("hub")
	require.ErrorContains(t, err, "failed to load kubeconfig")
}

func TestNoKubeconfigPathIsAnError(t *testing.T) {
	t.Parallel()

	service := newKubeConfigService(zap.NewNop(), envOf(map[string]string{}), inClusterProbe(false))

	_, err := service.GetClusters()
	require.ErrorContains(t, err, "kubeconfig path not found")
}

func TestKubeconfigEditsArePickedUpWithoutRestart(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, "kubeconfig", spokeBKubeconfig)
	service := newKubeConfigService(zap.NewNop(), envOf(map[string]string{EnvKubeconfig: path}), inClusterProbe(true))

	clusters, err := service.GetClusters()
	require.NoError(t, err)
	assert.Equal(t, []string{"spoke-b"}, clusterNames(clusters))

	err = os.WriteFile(path, []byte(hubAndSpokeKubeconfig), 0o600)
	require.NoError(t, err)

	clusters, err = service.GetClusters()
	require.NoError(t, err)
	assert.Equal(t, []string{"hub", "spoke-a"}, clusterNames(clusters), "a rewritten mounted kubeconfig shows on the next request")
}

func TestHomeDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "HOME", env: map[string]string{"HOME": "/home/me", "USERPROFILE": `C:\Users\me`}, want: "/home/me"},
		{name: "USERPROFILE fallback", env: map[string]string{"USERPROFILE": `C:\Users\me`}, want: `C:\Users\me`},
		{name: "neither", env: map[string]string{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, homeDir(envOf(tt.env)))
		})
	}
}
