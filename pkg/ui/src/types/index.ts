export interface PodInfo {
  name: string;
  namespace: string;
  imageTag: string;
  status: string;
  ready: string;
  restarts: number;
  age: string;
  node: string;
  ip: string;
  labels?: Record<string, string>;
}

export interface PodsResponse {
  pods: PodInfo[];
}

export interface ClusterInfo {
  name: string;
  current: boolean;
}

export interface ClustersResponse {
  inCluster: boolean;
  clusters: ClusterInfo[];
}

export interface NamespacesResponse {
  namespaces: string[];
}