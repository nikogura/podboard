import type { PodsResponse, NamespacesResponse, ClustersResponse } from '@/types';

const API_BASE = '/api';

class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public response?: unknown
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

async function fetchAPI<T>(endpoint: string, options?: RequestInit): Promise<T> {
  const url = `${API_BASE}${endpoint}`;

  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (!response.ok) {
    let errorMessage = `HTTP ${response.status}: ${response.statusText}`;
    let errorResponse;

    try {
      errorResponse = await response.json();
      errorMessage = errorResponse.error || errorMessage;
    } catch {
      // Response isn't JSON, use status text
    }

    throw new ApiError(errorMessage, response.status, errorResponse);
  }

  return response.json();
}

export const api = {
  // Clusters
  getClusters: (): Promise<ClustersResponse> =>
    fetchAPI('/clusters'),

  // Namespaces
  getNamespaces: (cluster?: string): Promise<NamespacesResponse> => {
    const params = new URLSearchParams();
    if (cluster) params.append('cluster', cluster);

    const queryString = params.toString();
    return fetchAPI(`/namespaces${queryString ? `?${queryString}` : ''}`);
  },

  // Pods
  getPods: (namespace?: string, labelSelector?: string, cluster?: string): Promise<PodsResponse> => {
    const params = new URLSearchParams();
    if (namespace) params.append('namespace', namespace);
    if (labelSelector) params.append('labelSelector', labelSelector);
    if (cluster) params.append('cluster', cluster);

    const queryString = params.toString();
    return fetchAPI(`/pods${queryString ? `?${queryString}` : ''}`);
  },

  // Delete pod
  deletePod: (namespace: string, podName: string, cluster?: string): Promise<{message: string}> => {
    const params = new URLSearchParams();
    if (cluster) params.append('cluster', cluster);

    const queryString = params.toString();
    return fetchAPI(`/pods/${namespace}/${podName}${queryString ? `?${queryString}` : ''}`, {
      method: 'DELETE'
    });
  },
};

export { ApiError };