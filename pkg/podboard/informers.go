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
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	// EnvInformerSyncTimeout names the environment variable bounding the initial cache sync.
	EnvInformerSyncTimeout = "PODBOARD_INFORMER_SYNC_TIMEOUT"
	// EnvInformerIdleTimeout names the environment variable controlling idle informer eviction.
	EnvInformerIdleTimeout = "PODBOARD_INFORMER_IDLE_TIMEOUT"

	defaultInformerSyncTimeout = 30 * time.Second
	defaultInformerIdleTimeout = 10 * time.Minute
	minReapInterval            = time.Second
	reapIntervalDivisor        = 4
)

// ClusterClientFactory builds a Kubernetes client for the named cluster.
type ClusterClientFactory func(clusterName string) (client kubernetes.Interface, err error)

// InformerConfig tunes the per-cluster pod informers.
type InformerConfig struct {
	// SyncTimeout bounds how long the first request for a cluster waits for the
	// pod cache to fill before failing.
	SyncTimeout time.Duration
	// IdleTimeout evicts a cluster's informer, and its watch, after this long
	// without a request. Zero or negative disables eviction.
	IdleTimeout time.Duration
}

// DefaultInformerConfig returns the informer settings used when nothing is configured.
func DefaultInformerConfig() (config InformerConfig) {
	config = InformerConfig{
		SyncTimeout: defaultInformerSyncTimeout,
		IdleTimeout: defaultInformerIdleTimeout,
	}
	return config
}

// InformerConfigFromEnv returns the defaults overridden by PODBOARD_INFORMER_SYNC_TIMEOUT
// and PODBOARD_INFORMER_IDLE_TIMEOUT, both Go duration strings.
func InformerConfigFromEnv() (config InformerConfig, err error) {
	config = DefaultInformerConfig()

	config.SyncTimeout, err = durationFromEnv(EnvInformerSyncTimeout, config.SyncTimeout)
	if err != nil {
		return config, err
	}

	config.IdleTimeout, err = durationFromEnv(EnvInformerIdleTimeout, config.IdleTimeout)
	if err != nil {
		return config, err
	}

	return config, err
}

// durationFromEnv parses the named variable as a duration, returning fallback when it is unset.
func durationFromEnv(name string, fallback time.Duration) (value time.Duration, err error) {
	value = fallback

	raw := os.Getenv(name)
	if raw == "" {
		return value, err
	}

	value, err = time.ParseDuration(raw)
	if err != nil {
		value = fallback
		err = fmt.Errorf("%s=%q is not a duration: %w", name, raw, err)
		return value, err
	}

	return value, err
}

// clusterInformer is one cluster's pod informer and the state needed to share it.
type clusterInformer struct {
	factory  informers.SharedInformerFactory
	informer cache.SharedIndexInformer
	lister   listerscorev1.PodLister
	// stopCh stops the informer's watch when closed.
	stopCh chan struct{}
	// ready is closed once the cache has synced or startup has failed; err is
	// written before it closes.
	ready chan struct{}
	err   error
	// lastUsed is guarded by InformerManager.mu.
	lastUsed time.Time
}

// InformerManager serves pod reads from one shared informer per cluster.
//
// The first request for a cluster builds its client, starts a pod informer and
// waits for the cache to sync. Later requests read the in-memory cache, so the
// API server sees a single long-lived watch per cluster regardless of how many
// viewers poll or how often. Clusters nobody has asked about for IdleTimeout
// are evicted so idle clusters hold no watch.
type InformerManager struct {
	clientFactory ClusterClientFactory
	config        InformerConfig
	logger        *zap.Logger
	now           func() (current time.Time)
	reapInterval  time.Duration

	mu       sync.Mutex
	clusters map[string]*clusterInformer

	cancelReaper context.CancelFunc
	reaperDone   chan struct{}
}

// NewInformerManager creates a manager that builds clients with clientFactory.
func NewInformerManager(clientFactory ClusterClientFactory, config InformerConfig, logger *zap.Logger) (manager *InformerManager) {
	reapInterval := config.IdleTimeout / reapIntervalDivisor
	if reapInterval < minReapInterval {
		reapInterval = minReapInterval
	}

	manager = &InformerManager{
		clientFactory: clientFactory,
		config:        config,
		logger:        logger,
		now:           time.Now,
		reapInterval:  reapInterval,
		clusters:      make(map[string]*clusterInformer),
	}
	return manager
}

// Start launches the idle reaper. It does nothing when eviction is disabled.
func (im *InformerManager) Start(ctx context.Context) {
	if im.config.IdleTimeout <= 0 {
		return
	}

	reaperCtx, cancel := context.WithCancel(ctx)
	im.cancelReaper = cancel
	im.reaperDone = make(chan struct{})

	go im.runReaper(reaperCtx)
}

// Stop halts the reaper and every informer. It is safe to call more than once.
func (im *InformerManager) Stop() {
	if im.cancelReaper != nil {
		im.cancelReaper()
		<-im.reaperDone
		im.cancelReaper = nil
	}

	im.mu.Lock()
	entries := make([]*clusterInformer, 0, len(im.clusters))
	names := make([]string, 0, len(im.clusters))
	for name, entry := range im.clusters {
		entries = append(entries, entry)
		names = append(names, name)
	}
	im.clusters = make(map[string]*clusterInformer)
	im.mu.Unlock()

	for i, entry := range entries {
		im.stopEntry(names[i], entry)
	}
}

// PodLister returns a synced pod lister for the cluster, starting its informer on first use.
func (im *InformerManager) PodLister(ctx context.Context, clusterName string) (lister listerscorev1.PodLister, err error) {
	entry, created := im.getOrCreate(clusterName)
	if created {
		im.startEntry(clusterName, entry)
	}

	select {
	case <-entry.ready:
	case <-ctx.Done():
		err = fmt.Errorf("waiting for pod cache of cluster %q: %w", clusterName, ctx.Err())
		return lister, err
	}

	if entry.err != nil {
		err = entry.err
		return lister, err
	}

	lister = entry.lister
	return lister, err
}

// ActiveClusters returns the names of clusters that currently hold an informer, sorted.
func (im *InformerManager) ActiveClusters() (names []string) {
	im.mu.Lock()
	defer im.mu.Unlock()

	names = make([]string, 0, len(im.clusters))
	for name := range im.clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// getOrCreate returns the cluster's entry, registering a new unsynced one when
// absent. created reports whether the caller must start it.
func (im *InformerManager) getOrCreate(clusterName string) (entry *clusterInformer, created bool) {
	im.mu.Lock()
	defer im.mu.Unlock()

	now := im.now()

	existing, exists := im.clusters[clusterName]
	if exists {
		existing.lastUsed = now
		entry = existing
		return entry, created
	}

	entry = &clusterInformer{
		stopCh:   make(chan struct{}),
		ready:    make(chan struct{}),
		lastUsed: now,
	}
	im.clusters[clusterName] = entry
	created = true
	return entry, created
}

// startEntry builds and syncs the informer, then signals waiters. A failed
// start is removed from the registry so the next request retries.
func (im *InformerManager) startEntry(clusterName string, entry *clusterInformer) {
	err := im.buildInformer(clusterName, entry)
	if err != nil {
		entry.err = err
		im.logger.Warn("Pod informer failed to start", zap.String("cluster", clusterName), zap.Error(err))

		im.mu.Lock()
		if im.clusters[clusterName] == entry {
			delete(im.clusters, clusterName)
		}
		im.mu.Unlock()
	}

	close(entry.ready)
}

// buildInformer creates the client and informer for the cluster and waits for
// its cache to sync.
func (im *InformerManager) buildInformer(clusterName string, entry *clusterInformer) (err error) {
	client, clientErr := im.clientFactory(clusterName)
	if clientErr != nil {
		err = fmt.Errorf("creating client for cluster %q: %w", clusterName, clientErr)
		return err
	}

	factory := informers.NewSharedInformerFactory(client, 0)
	podInformer := factory.Core().V1().Pods()
	informer := podInformer.Informer()

	err = informer.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *cache.Reflector, watchErr error) {
		im.logger.Warn("Pod watch dropped; serving cached state until it reconnects", zap.String("cluster", clusterName), zap.Error(watchErr))
	})
	if err != nil {
		err = fmt.Errorf("installing watch error handler for cluster %q: %w", clusterName, err)
		return err
	}

	factory.Start(entry.stopCh)

	syncCtx, cancel := context.WithTimeout(context.Background(), im.config.SyncTimeout)
	defer cancel()

	if !cache.WaitForCacheSync(syncCtx.Done(), informer.HasSynced) {
		close(entry.stopCh)
		// Shutdown waits for the reflector, which may be stuck in a hung LIST;
		// never block a request on it.
		go factory.Shutdown()

		err = fmt.Errorf("pod cache for cluster %q did not sync within %s", clusterName, im.config.SyncTimeout)
		return err
	}

	entry.factory = factory
	entry.informer = informer
	entry.lister = podInformer.Lister()

	im.logger.Info("Pod informer started", zap.String("cluster", clusterName))
	return err
}

// runReaper evicts idle informers on a timer until ctx is cancelled.
func (im *InformerManager) runReaper(ctx context.Context) {
	defer close(im.reaperDone)

	ticker := time.NewTicker(im.reapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			im.reapIdle(im.now())
		}
	}
}

// reapIdle stops and forgets every synced informer unused for IdleTimeout as
// of now, returning the evicted cluster names.
func (im *InformerManager) reapIdle(now time.Time) (evicted []string) {
	if im.config.IdleTimeout <= 0 {
		return evicted
	}

	im.mu.Lock()
	idle := make([]*clusterInformer, 0)
	for name, entry := range im.clusters {
		if !entry.isReady() || entry.err != nil {
			continue
		}
		if now.Sub(entry.lastUsed) < im.config.IdleTimeout {
			continue
		}
		delete(im.clusters, name)
		idle = append(idle, entry)
		evicted = append(evicted, name)
	}
	im.mu.Unlock()

	for i, entry := range idle {
		im.stopEntry(evicted[i], entry)
	}

	sort.Strings(evicted)
	return evicted
}

// stopEntry closes a synced informer's watch and waits for its goroutines.
func (im *InformerManager) stopEntry(clusterName string, entry *clusterInformer) {
	if !entry.isReady() || entry.err != nil {
		return
	}

	close(entry.stopCh)
	entry.factory.Shutdown()

	im.logger.Info("Pod informer stopped", zap.String("cluster", clusterName))
}

// isReady reports whether startup has finished, successfully or not.
func (ci *clusterInformer) isReady() (ready bool) {
	select {
	case <-ci.ready:
		ready = true
	default:
	}
	return ready
}
