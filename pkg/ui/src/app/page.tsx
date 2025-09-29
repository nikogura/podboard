'use client';

import React, { useState, useEffect, useCallback } from 'react';
import { SimpleLayout } from '@/components/SimpleLayout';
import type { PodInfo, ClusterInfo } from '@/types';
import { api, ApiError } from '@/lib/api';

export default function HomePage() {
  const [pods, setPods] = useState<PodInfo[]>([]);
  const [namespaces, setNamespaces] = useState<string[]>([]);
  const [clusters, setClusters] = useState<ClusterInfo[]>([]);
  const [selectedCluster, setSelectedCluster] = useState<string>('');
  const [selectedNamespace, setSelectedNamespace] = useState<string>('default');
  const [selectedLabelFilter, setSelectedLabelFilter] = useState<string>('');
  const [refreshInterval, setRefreshInterval] = useState<number>(2);
  const [lastUpdate, setLastUpdate] = useState<Date | null>(null);
  const [loading, setLoading] = useState(true);
  const [inCluster, setInCluster] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  // Fetch clusters and initialize on mount
  useEffect(() => {
    const initializeApp = async () => {
      try {
        // First, get clusters
        const clustersResponse = await api.getClusters();
        setInCluster(clustersResponse.inCluster);

        if (!clustersResponse.inCluster) {
          setClusters(clustersResponse.clusters);
          // Set initial cluster to current cluster or first available
          const currentCluster = clustersResponse.clusters.find(c => c.current);
          if (currentCluster) {
            setSelectedCluster(currentCluster.name);
          } else if (clustersResponse.clusters.length > 0) {
            setSelectedCluster(clustersResponse.clusters[0].name);
          }
        } else {
          // In cluster mode, set empty cluster
          setSelectedCluster('');
        }
      } catch (err) {
        console.error('Failed to fetch clusters:', err);
        setError('Failed to fetch clusters');
      } finally {
        setLoading(false);
      }
    };

    initializeApp();
  }, []);

  // Fetch namespaces when cluster changes
  useEffect(() => {
    if (loading) return;

    const fetchNamespaces = async () => {
      try {
        const response = await api.getNamespaces(selectedCluster || undefined);
        setNamespaces(response.namespaces);
        setError(null);

        // Auto-set namespace to "default" if available, otherwise first namespace
        let newNamespace = 'default';
        if (response.namespaces.includes("default")) {
          newNamespace = "default";
        } else if (response.namespaces.length > 0) {
          newNamespace = response.namespaces[0];
        }

        // Only update namespace if it's different from current selection
        setSelectedNamespace(currentNamespace => {
          if (newNamespace !== currentNamespace) {
            return newNamespace;
          }
          return currentNamespace;
        });
      } catch (err) {
        console.error('Failed to fetch namespaces for cluster:', selectedCluster, err);
        // Set error but don't crash - try to keep previous namespaces
        if (err instanceof ApiError) {
          setError(`Failed to fetch namespaces for cluster ${selectedCluster}: ${err.message}`);
        } else {
          setError(`Failed to fetch namespaces for cluster ${selectedCluster}`);
        }

        // If we have no namespaces at all, set a fallback
        if (namespaces.length === 0) {
          setNamespaces(['default']);
          setSelectedNamespace('default');
        }
      }
    };

    fetchNamespaces();
  }, [selectedCluster, loading, namespaces.length]);

  // Fetch pods periodically
  const fetchPods = useCallback(async () => {
    try {
      const response = await api.getPods(
        selectedNamespace,
        selectedLabelFilter || undefined,
        selectedCluster || undefined
      );
      setPods(response.pods);
      setLastUpdate(new Date());
      setError(null);
    } catch (err) {
      console.error('Failed to fetch pods:', err);
      setError('Failed to fetch pods');
    }
  }, [selectedNamespace, selectedLabelFilter, selectedCluster]);

  // Initial pod fetch and interval setup
  useEffect(() => {
    if (loading) return;

    fetchPods();
    const interval = setInterval(fetchPods, refreshInterval * 1000);
    return () => clearInterval(interval);
  }, [selectedCluster, selectedNamespace, selectedLabelFilter, refreshInterval, loading, fetchPods]);

  if (loading) {
    return (
      <SimpleLayout>
        <div style={{ textAlign: "center", padding: "2rem" }}>
          Loading...
        </div>
      </SimpleLayout>
    );
  }

  const getStatusColor = (status: string) => {
    switch (status.toLowerCase()) {
      case 'running': return '#28a745';
      case 'pending': return '#ffc107';
      case 'failed': case 'error': return '#dc3545';
      case 'succeeded': return '#28a745';
      default: return '#6c757d';
    }
  };

  const handleDeletePod = async (pod: PodInfo) => {
    if (!confirm(`Are you sure you want to delete pod ${pod.name}?`)) {
      return;
    }

    try {
      await api.deletePod(pod.namespace, pod.name, selectedCluster || undefined);
      // Refresh pods list immediately
      fetchPods();
    } catch (err) {
      console.error('Failed to delete pod:', err);
      if (err instanceof ApiError) {
        alert(`Failed to delete pod: ${err.message}`);
      } else {
        alert('Failed to delete pod');
      }
    }
  };

  return (
    <SimpleLayout
      environment={selectedNamespace}
    >
      {/* Controls */}
      <div style={{
        display: "flex",
        gap: "1rem",
        marginBottom: "1rem",
        alignItems: "center",
        flexWrap: "wrap"
      }}>
        {/* Cluster dropdown - only show if not in cluster */}
        {!inCluster && (
          <div>
            <label style={{ marginRight: "0.5rem", fontSize: "0.875rem" }}>Cluster:</label>
            <select
              value={selectedCluster}
              onChange={(e) => setSelectedCluster(e.target.value)}
              style={{
                padding: "0.25rem 0.5rem",
                border: "1px solid var(--border-color)",
                borderRadius: "4px",
                backgroundColor: "var(--bg-color)",
                color: "var(--text-color)"
              }}
            >
              {clusters.map(cluster => (
                <option key={cluster.name} value={cluster.name}>
                  {cluster.name} {cluster.current ? "(current)" : ""}
                </option>
              ))}
            </select>
          </div>
        )}

        <div>
          <label style={{ marginRight: "0.5rem", fontSize: "0.875rem" }}>Namespace:</label>
          <select
            value={selectedNamespace}
            onChange={(e) => setSelectedNamespace(e.target.value)}
            style={{
              padding: "0.25rem 0.5rem",
              border: "1px solid var(--border-color)",
              borderRadius: "4px",
              backgroundColor: "var(--bg-color)",
              color: "var(--text-color)"
            }}
          >
            {namespaces.map(ns => (
              <option key={ns} value={ns}>{ns}</option>
            ))}
          </select>
        </div>

        <div>
          <label style={{ marginRight: "0.5rem", fontSize: "0.875rem" }}>Refresh:</label>
          <select
            value={refreshInterval}
            onChange={(e) => setRefreshInterval(Number(e.target.value))}
            style={{
              padding: "0.25rem 0.5rem",
              border: "1px solid var(--border-color)",
              borderRadius: "4px",
              backgroundColor: "var(--bg-color)",
              color: "var(--text-color)"
            }}
          >
            <option value={1}>1s</option>
            <option value={2}>2s</option>
            <option value={5}>5s</option>
            <option value={10}>10s</option>
            <option value={30}>30s</option>
          </select>
        </div>

        <div>
          <label style={{ marginRight: "0.5rem", fontSize: "0.875rem" }}>Label Filter:</label>
          <input
            type="text"
            value={selectedLabelFilter}
            onChange={(e) => setSelectedLabelFilter(e.target.value)}
            placeholder="e.g., app=nginx, app=~coxex.*, env=~dev|staging"
            style={{
              padding: "0.25rem 0.5rem",
              border: "1px solid var(--border-color)",
              borderRadius: "4px",
              backgroundColor: "var(--bg-color)",
              color: "var(--text-color)",
              minWidth: "250px"
            }}
          />
        </div>
      </div>

      {/* Status Info */}
      <div style={{
        display: "flex",
        justifyContent: "space-between",
        alignItems: "center",
        marginBottom: "1rem",
        fontSize: "0.875rem",
        color: "var(--text-muted)"
      }}>
        <div>
          {lastUpdate ? (
            <>Last updated: {lastUpdate.toLocaleTimeString()}</>
          ) : (
            "Loading..."
          )}
        </div>
        <div>
          {(pods || []).length} pod{(pods || []).length !== 1 ? 's' : ''}
        </div>
      </div>

      {error && (
        <div style={{
          backgroundColor: "rgba(220, 53, 69, 0.1)",
          border: "1px solid #dc3545",
          borderRadius: "8px",
          padding: "1rem",
          marginBottom: "1rem",
          color: "#dc3545"
        }}>
          {error}
        </div>
      )}

      {/* Pods Table */}
      <div style={{
        backgroundColor: "var(--bg-color)",
        border: "1px solid var(--border-color)",
        borderRadius: "8px",
        overflow: "hidden"
      }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr style={{ backgroundColor: "var(--table-header-bg)" }}>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>Name</th>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>Version</th>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>Status</th>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>Ready</th>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>Restarts</th>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>Age</th>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>Node</th>
              <th style={{ padding: "0.75rem", textAlign: "left", fontWeight: "600" }}>IP</th>
              <th style={{ padding: "0.75rem", textAlign: "center", fontWeight: "600" }}>Delete</th>
            </tr>
          </thead>
          <tbody>
            {(pods || []).map((pod, index) => (
              <tr key={`${pod.namespace || 'unknown'}-${pod.name || 'unknown'}`} style={{
                borderTop: index > 0 ? "1px solid var(--border-color)" : "none"
              }}>
                <td style={{ padding: "0.75rem", fontFamily: "monospace" }}>{pod.name || '-'}</td>
                <td style={{ padding: "0.75rem", fontFamily: "monospace", fontSize: "0.875rem", color: "var(--text-muted)" }}>{pod.imageTag || '-'}</td>
                <td style={{ padding: "0.75rem" }}>
                  <span style={{
                    color: getStatusColor(pod.status || 'unknown'),
                    fontWeight: "500"
                  }}>
                    {pod.status || 'Unknown'}
                  </span>
                </td>
                <td style={{ padding: "0.75rem", fontFamily: "monospace" }}>{pod.ready || '-'}</td>
                <td style={{ padding: "0.75rem", textAlign: "center" }}>{pod.restarts || 0}</td>
                <td style={{ padding: "0.75rem" }}>{pod.age || '-'}</td>
                <td style={{ padding: "0.75rem", fontSize: "0.875rem" }}>{pod.node || '-'}</td>
                <td style={{ padding: "0.75rem", fontFamily: "monospace", fontSize: "0.875rem" }}>{pod.ip || '-'}</td>
                <td style={{ padding: "0.75rem", textAlign: "center" }}>
                  <button
                    onClick={() => handleDeletePod(pod)}
                    style={{
                      padding: "0.25rem 0.5rem",
                      backgroundColor: "#dc3545",
                      color: "white",
                      border: "none",
                      borderRadius: "4px",
                      fontSize: "0.75rem",
                      cursor: "pointer",
                      fontWeight: "500"
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.backgroundColor = "#c82333";
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.backgroundColor = "#dc3545";
                    }}
                    title={`Delete pod ${pod.name}`}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {(pods || []).length === 0 && !error && (
          <div style={{ padding: "2rem", textAlign: "center", color: "var(--text-muted)" }}>
            No pods found in namespace &quot;{selectedNamespace}&quot;
          </div>
        )}
      </div>
    </SimpleLayout>
  );
}