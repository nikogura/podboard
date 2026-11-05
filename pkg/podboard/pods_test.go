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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// newTestPodService builds a PodService over a single fake cluster named
// alpha whose API starts out holding pods.
func newTestPodService(t *testing.T, pods ...*corev1.Pod) (service *PodService, client *fake.Clientset) {
	t.Helper()

	clusters := newFakeClusters()
	client = clusters.add(testClusterAlpha, pods...)
	manager := newTestManager(t, clusters, DefaultInformerConfig())

	service = NewPodService(&KubeConfigService{logger: zap.NewNop()}, manager, zap.NewNop())
	return service, client
}

// infoKeys returns namespace/name for each PodInfo in the order given.
func infoKeys(infos []PodInfo) (keys []string) {
	keys = make([]string, 0, len(infos))
	for _, info := range infos {
		keys = append(keys, info.Namespace+"/"+info.Name)
	}
	return keys
}

func TestGetPodsScopesByNamespace(t *testing.T) {
	t.Parallel()

	service, _ := newTestPodService(t,
		testPod(testNSDefault, "web-1", nil),
		testPod(testNSDefault, "web-2", nil),
		testPod(testNSSystem, "coredns", nil),
	)

	tests := []struct {
		namespace string
		want      []string
	}{
		{namespace: testNSDefault, want: []string{"default/web-1", "default/web-2"}},
		{namespace: testNSSystem, want: []string{"kube-system/coredns"}},
		{namespace: allNamespaces, want: []string{"default/web-1", "default/web-2", "kube-system/coredns"}},
		{namespace: "nothing-here", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			t.Parallel()

			pods, err := service.GetPods(t.Context(), testClusterAlpha, tt.namespace, "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, infoKeys(pods))
		})
	}
}

func TestGetPodsSortsByNamespaceThenName(t *testing.T) {
	t.Parallel()

	service, _ := newTestPodService(t,
		testPod("zeta", "a", nil),
		testPod(testNSDefault, "c", nil),
		testPod("alpha", "b", nil),
		testPod(testNSDefault, "a", nil),
	)

	for range 5 {
		pods, err := service.GetPods(t.Context(), testClusterAlpha, allNamespaces, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"alpha/b", "default/a", "default/c", "zeta/a"}, infoKeys(pods), "order must be stable across polls")
	}
}

func TestGetPodsAppliesLabelSelectors(t *testing.T) {
	t.Parallel()

	service, _ := newTestPodService(t,
		testPod(testNSDefault, "web", map[string]string{"app": "web", "tier": "frontend"}),
		testPod(testNSDefault, "api", map[string]string{"app": "api", "tier": "backend"}),
		testPod(testNSDefault, "worker", map[string]string{"app": "worker-batch"}),
	)

	tests := []struct {
		name     string
		selector string
		want     []string
	}{
		{name: "empty matches all", selector: "", want: []string{"default/api", "default/web", "default/worker"}},
		{name: "equality", selector: "app=web", want: []string{"default/web"}},
		{name: "set", selector: "app in (web,api)", want: []string{"default/api", "default/web"}},
		{name: "inequality", selector: "app!=web", want: []string{"default/api", "default/worker"}},
		{name: "existence", selector: "tier", want: []string{"default/api", "default/web"}},
		{name: "regex", selector: "app=~w.*", want: []string{"default/web", "default/worker"}},
		{name: "regex with equality", selector: "app=~w.*,tier=frontend", want: []string{"default/web"}},
		{name: "regex alternation", selector: "app=~^(api|worker-batch)$", want: []string{"default/api", "default/worker"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pods, err := service.GetPods(t.Context(), testClusterAlpha, testNSDefault, tt.selector)
			require.NoError(t, err)
			assert.Equal(t, tt.want, infoKeys(pods))
		})
	}
}

func TestGetPodsRejectsInvalidLabelSelector(t *testing.T) {
	t.Parallel()

	service, _ := newTestPodService(t, testPod(testNSDefault, "web", map[string]string{"app": "web"}))

	_, err := service.GetPods(t.Context(), testClusterAlpha, testNSDefault, "app==web=")
	require.ErrorContains(t, err, `invalid label selector "app==web="`)
}

func TestGetPodsReflectsCacheUpdates(t *testing.T) {
	t.Parallel()

	service, client := newTestPodService(t, testPod(testNSDefault, "web-1", nil))

	pods, err := service.GetPods(t.Context(), testClusterAlpha, testNSDefault, "")
	require.NoError(t, err)
	require.Equal(t, []string{"default/web-1"}, infoKeys(pods))

	_, err = client.CoreV1().Pods(testNSDefault).Create(t.Context(), testPod(testNSDefault, "web-2", nil), metav1.CreateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() (seen bool) {
		current, getErr := service.GetPods(t.Context(), testClusterAlpha, testNSDefault, "")
		seen = getErr == nil && len(current) == 2
		return seen
	}, eventuallyWait, eventuallyTick)

	err = client.CoreV1().Pods(testNSDefault).Delete(t.Context(), "web-1", metav1.DeleteOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() (gone bool) {
		current, getErr := service.GetPods(t.Context(), testClusterAlpha, testNSDefault, "")
		gone = getErr == nil && len(current) == 1 && current[0].Name == "web-2"
		return gone
	}, eventuallyWait, eventuallyTick)
}

func TestGetPodsDoesNotListPerRequest(t *testing.T) {
	t.Parallel()

	service, client := newTestPodService(t, testPod(testNSDefault, "web-1", nil))

	for range 100 {
		_, err := service.GetPods(t.Context(), testClusterAlpha, allNamespaces, "")
		require.NoError(t, err)
	}

	assert.Equal(t, 1, countPodLists(client), "the API server must see one LIST regardless of poll count")
}

func TestGetPodsReportsClusterItCannotReach(t *testing.T) {
	t.Parallel()

	service, _ := newTestPodService(t)

	_, err := service.GetPods(t.Context(), "ghost", testNSDefault, "")
	require.ErrorContains(t, err, "failed to get pod cache")
	require.ErrorContains(t, err, `cluster "ghost" not found in kubeconfig`)
}

func TestGetPodsNeedsAResolvableCluster(t *testing.T) {
	t.Parallel()

	service, _ := newTestPodService(t)

	_, err := service.GetPods(t.Context(), "", testNSDefault, "")
	require.ErrorContains(t, err, "failed to resolve cluster")
	require.ErrorContains(t, err, "no cluster specified")
}

func TestResolveClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inCluster bool
		requested string
		want      string
	}{
		{name: "explicit cluster passes through", requested: "prod", want: "prod"},
		{name: "in-cluster ignores the requested name", inCluster: true, requested: "prod", want: inClusterName},
		{name: "in-cluster with no name", inCluster: true, want: inClusterName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewPodService(&KubeConfigService{logger: zap.NewNop(), inCluster: tt.inCluster}, nil, zap.NewNop())

			got, err := service.resolveClusterName(tt.requested)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetPodsBuildsPodInfoFromCachedObjects(t *testing.T) {
	t.Parallel()

	pod := testPod(testNSDefault, "web-1", map[string]string{"app": "web"})
	pod.CreationTimestamp = metav1.NewTime(time.Now().Add(-3 * time.Hour))
	pod.Spec.NodeName = "node-a"
	pod.Spec.Containers = []corev1.Container{{Name: "web", Image: "registry.example.com:5000/web:v1.2.3"}}
	pod.Status.Phase = corev1.PodRunning
	pod.Status.PodIP = "10.0.0.7"
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:         "web",
		Ready:        false,
		RestartCount: 4,
		State:        corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
	}}

	service, _ := newTestPodService(t, pod)

	pods, err := service.GetPods(t.Context(), testClusterAlpha, testNSDefault, "app=web")
	require.NoError(t, err)
	require.Len(t, pods, 1)

	info := pods[0]
	assert.Equal(t, "web-1", info.Name)
	assert.Equal(t, "v1.2.3", info.ImageTag)
	assert.Equal(t, "CrashLoopBackOff", info.Status)
	assert.Equal(t, "0/1", info.Ready)
	assert.Equal(t, int32(4), info.Restarts)
	assert.Equal(t, "3h", info.Age)
	assert.Equal(t, "node-a", info.Node)
	assert.Equal(t, "10.0.0.7", info.IP)
	assert.Equal(t, map[string]string{"app": "web"}, info.Labels)
}
