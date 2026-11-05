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
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	testClusterAlpha = "alpha"
	testClusterBeta  = "beta"
	testClusterSlow  = "slow"
	testNSDefault    = "default"
	testNSSystem     = "kube-system"
	eventuallyWait   = 5 * time.Second
	eventuallyTick   = 5 * time.Millisecond
)

// fakeClusters hands out one fake clientset per cluster name and counts how
// often each was built, standing in for the kubeconfig-backed client factory.
type fakeClusters struct {
	mu      sync.Mutex
	clients map[string]*fake.Clientset
	errs    map[string]error
	builds  map[string]int
}

func newFakeClusters() (clusters *fakeClusters) {
	clusters = &fakeClusters{
		clients: make(map[string]*fake.Clientset),
		errs:    make(map[string]error),
		builds:  make(map[string]int),
	}
	return clusters
}

// add registers a cluster whose API starts out holding pods.
func (fc *fakeClusters) add(name string, pods ...*corev1.Pod) (client *fake.Clientset) {
	objects := make([]runtime.Object, 0, len(pods))
	for _, pod := range pods {
		objects = append(objects, pod)
	}

	client = fake.NewClientset(objects...)

	fc.mu.Lock()
	fc.clients[name] = client
	fc.mu.Unlock()

	return client
}

// fail makes every build of the named cluster return err.
func (fc *fakeClusters) fail(name string, err error) {
	fc.mu.Lock()
	fc.errs[name] = err
	fc.mu.Unlock()
}

// factory satisfies ClusterClientFactory.
func (fc *fakeClusters) factory(name string) (client kubernetes.Interface, err error) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.builds[name]++

	if failErr, failing := fc.errs[name]; failing {
		err = failErr
		return client, err
	}

	fakeClient, exists := fc.clients[name]
	if !exists {
		err = fmt.Errorf("cluster %q not found in kubeconfig", name)
		return client, err
	}

	client = fakeClient
	return client, err
}

func (fc *fakeClusters) buildCount(name string) (count int) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	count = fc.builds[name]
	return count
}

func newTestManager(t *testing.T, clusters *fakeClusters, config InformerConfig) (manager *InformerManager) {
	t.Helper()

	manager = NewInformerManager(clusters.factory, config, zap.NewNop())
	t.Cleanup(manager.Stop)

	return manager
}

func testPod(namespace, name string, podLabels map[string]string) (pod *corev1.Pod) {
	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    podLabels,
		},
	}
	return pod
}

// podKeys returns namespace/name for each pod, sorted.
func podKeys(pods []*corev1.Pod) (keys []string) {
	keys = make([]string, 0, len(pods))
	for _, pod := range pods {
		keys = append(keys, pod.Namespace+"/"+pod.Name)
	}
	sort.Strings(keys)
	return keys
}

// countPodLists counts LIST calls the fake API server saw for pods.
func countPodLists(client *fake.Clientset) (count int) {
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "pods" {
			count++
		}
	}
	return count
}

// blockPodLists stalls every pod LIST against client until the returned
// release function is called. release is safe to call more than once.
func blockPodLists(t *testing.T, client *fake.Clientset) (release func()) {
	t.Helper()

	gate := make(chan struct{})
	var once sync.Once
	release = func() {
		once.Do(func() { close(gate) })
	}
	t.Cleanup(release)

	client.PrependReactor("list", "pods", func(_ k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		<-gate
		return handled, ret, err
	})

	return release
}

// listAll reads every pod from the cluster's cache through the manager.
func listAll(t *testing.T, manager *InformerManager, cluster string) (pods []*corev1.Pod) {
	t.Helper()

	lister, err := manager.PodLister(t.Context(), cluster)
	require.NoError(t, err)

	pods, err = lister.List(labels.Everything())
	require.NoError(t, err)

	return pods
}

// waitRegistered blocks until the manager holds an entry for the cluster.
func waitRegistered(t *testing.T, manager *InformerManager, cluster string) {
	t.Helper()

	require.Eventually(t, func() (registered bool) {
		for _, name := range manager.ActiveClusters() {
			if name == cluster {
				registered = true
			}
		}
		return registered
	}, eventuallyWait, eventuallyTick)
}

func TestPodListerBuildsOneInformerPerCluster(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	clusters.add(testClusterAlpha, testPod(testNSDefault, "a-1", nil))
	clusters.add(testClusterBeta, testPod(testNSDefault, "b-1", nil), testPod(testNSSystem, "b-2", nil))
	manager := newTestManager(t, clusters, DefaultInformerConfig())

	for range 3 {
		assert.Equal(t, []string{"default/a-1"}, podKeys(listAll(t, manager, testClusterAlpha)))
		assert.Equal(t, []string{"default/b-1", "kube-system/b-2"}, podKeys(listAll(t, manager, testClusterBeta)))
	}

	assert.Equal(t, 1, clusters.buildCount(testClusterAlpha))
	assert.Equal(t, 1, clusters.buildCount(testClusterBeta))
	assert.Equal(t, []string{testClusterAlpha, testClusterBeta}, manager.ActiveClusters())
}

func TestPodListerConcurrentFirstUseBuildsOnce(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	clusters.add(testClusterAlpha, testPod(testNSDefault, "a-1", nil))
	manager := newTestManager(t, clusters, DefaultInformerConfig())

	const callers = 25
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.PodLister(t.Context(), testClusterAlpha)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, clusters.buildCount(testClusterAlpha))
	assert.Equal(t, []string{testClusterAlpha}, manager.ActiveClusters())
}

func TestPodListerUnknownClusterIsNotCached(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	manager := newTestManager(t, clusters, DefaultInformerConfig())

	for range 2 {
		_, err := manager.PodLister(t.Context(), "ghost")
		require.ErrorContains(t, err, `creating client for cluster "ghost"`)
		require.ErrorContains(t, err, "not found in kubeconfig")
	}

	assert.Equal(t, 2, clusters.buildCount("ghost"), "a failed build must be retried, not cached")
	assert.Empty(t, manager.ActiveClusters())
}

func TestPodListerWrapsFactoryError(t *testing.T) {
	t.Parallel()

	boom := errors.New("credentials expired")
	clusters := newFakeClusters()
	clusters.fail(testClusterAlpha, boom)
	manager := newTestManager(t, clusters, DefaultInformerConfig())

	_, err := manager.PodLister(t.Context(), testClusterAlpha)
	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, testClusterAlpha)
}

func TestPodListerSyncTimeoutIsReportedAndRetried(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	client := clusters.add(testClusterSlow)
	blockPodLists(t, client)
	manager := newTestManager(t, clusters, InformerConfig{SyncTimeout: 100 * time.Millisecond})

	for attempt := 1; attempt <= 2; attempt++ {
		_, err := manager.PodLister(t.Context(), testClusterSlow)
		require.ErrorContains(t, err, `pod cache for cluster "slow" did not sync within 100ms`)
		assert.Empty(t, manager.ActiveClusters())
		assert.Equal(t, attempt, clusters.buildCount(testClusterSlow))
	}
}

func TestPodListerWaiterHonorsItsOwnContext(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	client := clusters.add(testClusterSlow, testPod(testNSDefault, "s-1", nil))
	release := blockPodLists(t, client)
	manager := newTestManager(t, clusters, InformerConfig{SyncTimeout: eventuallyWait})

	creatorErr := make(chan error, 1)
	go func() {
		_, err := manager.PodLister(context.Background(), testClusterSlow)
		creatorErr <- err
	}()
	waitRegistered(t, manager, testClusterSlow)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := manager.PodLister(ctx, testClusterSlow)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, `waiting for pod cache of cluster "slow"`)

	release()
	require.NoError(t, <-creatorErr, "the first caller keeps building after a waiter gives up")
	assert.Equal(t, []string{"default/s-1"}, podKeys(listAll(t, manager, testClusterSlow)))
}

func TestPodListerServesWatchUpdatesWithoutRelisting(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	client := clusters.add(testClusterAlpha, testPod(testNSDefault, "a-1", nil))
	manager := newTestManager(t, clusters, DefaultInformerConfig())

	assert.Equal(t, []string{"default/a-1"}, podKeys(listAll(t, manager, testClusterAlpha)))

	_, err := client.CoreV1().Pods(testNSDefault).Create(t.Context(), testPod(testNSDefault, "a-2", nil), metav1.CreateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() (seen bool) {
		seen = len(listAll(t, manager, testClusterAlpha)) == 2
		return seen
	}, eventuallyWait, eventuallyTick)

	for range 100 {
		listAll(t, manager, testClusterAlpha)
	}

	assert.Equal(t, 1, countPodLists(client), "polls must be served from cache, not by LISTing the API server")
}

func TestReapIdleEvictsOnlyIdleClusters(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	clusters.add("busy", testPod(testNSDefault, "x", nil))
	clusters.add("idle", testPod(testNSDefault, "y", nil))
	manager := newTestManager(t, clusters, InformerConfig{SyncTimeout: eventuallyWait, IdleTimeout: time.Minute})

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	manager.now = func() (current time.Time) {
		current = clock
		return current
	}

	listAll(t, manager, "busy")
	listAll(t, manager, "idle")

	manager.mu.Lock()
	idleInformer := manager.clusters["idle"].informer
	manager.mu.Unlock()

	clock = clock.Add(45 * time.Second)
	listAll(t, manager, "busy")

	clock = clock.Add(30 * time.Second)
	assert.Equal(t, []string{"idle"}, manager.reapIdle(clock))
	assert.Equal(t, []string{"busy"}, manager.ActiveClusters())
	assert.True(t, idleInformer.IsStopped(), "evicting must close the watch")

	assert.Equal(t, []string{"default/y"}, podKeys(listAll(t, manager, "idle")))
	assert.Equal(t, 2, clusters.buildCount("idle"), "the next request rebuilds an evicted informer")
	assert.Equal(t, 1, clusters.buildCount("busy"))
}

func TestReapIdleDisabledWhenIdleTimeoutIsZero(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	clusters.add(testClusterAlpha, testPod(testNSDefault, "a-1", nil))
	manager := newTestManager(t, clusters, InformerConfig{SyncTimeout: eventuallyWait})

	listAll(t, manager, testClusterAlpha)

	assert.Empty(t, manager.reapIdle(time.Now().Add(240*time.Hour)))
	assert.Equal(t, []string{testClusterAlpha}, manager.ActiveClusters())
}

func TestReapIdleSkipsClustersStillSyncing(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	client := clusters.add(testClusterSlow, testPod(testNSDefault, "s-1", nil))
	release := blockPodLists(t, client)
	manager := newTestManager(t, clusters, InformerConfig{SyncTimeout: eventuallyWait, IdleTimeout: time.Millisecond})

	creatorErr := make(chan error, 1)
	go func() {
		_, err := manager.PodLister(context.Background(), testClusterSlow)
		creatorErr <- err
	}()
	waitRegistered(t, manager, testClusterSlow)

	assert.Empty(t, manager.reapIdle(time.Now().Add(time.Hour)))
	assert.Equal(t, []string{testClusterSlow}, manager.ActiveClusters())

	release()
	require.NoError(t, <-creatorErr)
}

func TestStartRunsTheReaper(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	clusters.add(testClusterAlpha, testPod(testNSDefault, "a-1", nil))
	manager := newTestManager(t, clusters, InformerConfig{SyncTimeout: eventuallyWait, IdleTimeout: 20 * time.Millisecond})
	manager.reapInterval = 10 * time.Millisecond
	manager.Start(t.Context())

	listAll(t, manager, testClusterAlpha)

	require.Eventually(t, func() (empty bool) {
		empty = len(manager.ActiveClusters()) == 0
		return empty
	}, eventuallyWait, eventuallyTick)
}

func TestStartIsANoOpWithoutIdleTimeout(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, newFakeClusters(), InformerConfig{SyncTimeout: eventuallyWait})
	manager.Start(t.Context())

	assert.Nil(t, manager.cancelReaper)
}

func TestStopShutsDownEveryInformerAndIsIdempotent(t *testing.T) {
	t.Parallel()

	clusters := newFakeClusters()
	clusters.add(testClusterAlpha, testPod(testNSDefault, "a-1", nil))
	clusters.add(testClusterBeta, testPod(testNSDefault, "b-1", nil))
	manager := newTestManager(t, clusters, InformerConfig{SyncTimeout: eventuallyWait, IdleTimeout: time.Hour})
	manager.Start(t.Context())

	listAll(t, manager, testClusterAlpha)
	listAll(t, manager, testClusterBeta)

	manager.mu.Lock()
	alphaInformer := manager.clusters[testClusterAlpha].informer
	betaInformer := manager.clusters[testClusterBeta].informer
	manager.mu.Unlock()

	manager.Stop()
	manager.Stop()

	assert.Empty(t, manager.ActiveClusters())
	assert.True(t, alphaInformer.IsStopped())
	assert.True(t, betaInformer.IsStopped())

	assert.Equal(t, []string{"default/a-1"}, podKeys(listAll(t, manager, testClusterAlpha)), "the manager stays usable after Stop")
}

func TestNewInformerManagerDerivesReapInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		idle time.Duration
		want time.Duration
	}{
		{name: "quarter of the idle timeout", idle: 10 * time.Minute, want: 150 * time.Second},
		{name: "never below one second", idle: 2 * time.Second, want: time.Second},
		{name: "disabled still has a floor", idle: 0, want: time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manager := NewInformerManager(newFakeClusters().factory, InformerConfig{IdleTimeout: tt.idle}, zap.NewNop())
			assert.Equal(t, tt.want, manager.reapInterval)
		})
	}
}

func TestInformerConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		sync    string
		idle    string
		want    InformerConfig
		wantErr string
	}{
		{name: "defaults", want: DefaultInformerConfig()},
		{name: "overrides", sync: "5s", idle: "1m", want: InformerConfig{SyncTimeout: 5 * time.Second, IdleTimeout: time.Minute}},
		{name: "zero idle disables eviction", idle: "0", want: InformerConfig{SyncTimeout: defaultInformerSyncTimeout, IdleTimeout: 0}},
		{name: "bad sync timeout", sync: "soon", want: DefaultInformerConfig(), wantErr: EnvInformerSyncTimeout + `="soon" is not a duration`},
		{name: "bad idle timeout", idle: "later", want: DefaultInformerConfig(), wantErr: EnvInformerIdleTimeout + `="later" is not a duration`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvInformerSyncTimeout, tt.sync)
			t.Setenv(EnvInformerIdleTimeout, tt.idle)

			got, err := InformerConfigFromEnv()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
