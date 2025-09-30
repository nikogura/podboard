# Kubernetes RBAC Configuration for Podboard

This directory contains Kubernetes manifests for deploying podboard with appropriate RBAC permissions.

## RBAC Requirements

Podboard requires the following Kubernetes permissions:

### Core Permissions (Always Required)
- **pods (get, list, watch)**: Monitor pod status and receive real-time updates
- **namespaces (get, list)**: Discover available namespaces for filtering

### Optional Permissions
- **pods (delete)**: Allow pod deletion from the web interface
  - ⚠️ **Namespace-restricted recommended**: Limit deletion to specific namespaces
  - ⚠️ **Cluster-wide dangerous**: Allows deletion of pods in any namespace

## Security Considerations

### Namespace-Restricted Deployment (Recommended)
- Limits pod deletion to specific namespaces
- Reduces blast radius of accidental deletions
- Kubernetes will typically recreate deleted pods automatically
- Suitable for most monitoring and troubleshooting scenarios

### Cluster-Wide Deployment (Use with Caution)
- ⚠️ **WARNING**: Allows pod deletion across all namespaces
- Should only be used when administrative access is explicitly required
- Consider creating separate restricted deployments per namespace instead

## Available Configurations

1. **`rbac-namespace-restricted.yaml`**: Safe namespace-restricted RBAC
2. **`rbac-cluster-wide.yaml`**: Cluster-wide RBAC (use with caution)
3. **`deployment.yaml`**: Complete deployment with service account
4. **`service.yaml`**: Service for accessing the web interface

## Quick Deploy

### Namespace-Restricted (Recommended)
```bash
# Deploy in default namespace with restricted permissions
kubectl apply -f k8s/rbac-namespace-restricted.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml

# Access the interface
kubectl port-forward svc/podboard 9999:9999
```

### Cluster-Wide (Advanced Users Only)
```bash
# ⚠️ WARNING: This grants pod deletion across ALL namespaces
kubectl apply -f k8s/rbac-cluster-wide.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml

# Access the interface
kubectl port-forward svc/podboard 9999:9999
```

## Customization

### Different Namespace
```bash
# Create in monitoring namespace
kubectl create namespace monitoring
kubectl apply -f k8s/ -n monitoring

# Update ClusterRoleBinding subject namespace in rbac-cluster-wide.yaml if using cluster-wide RBAC
```

### Custom Service Account
All manifests use the `podboard` service account. To use a different name:
1. Update the `metadata.name` in the ServiceAccount resource
2. Update `spec.template.spec.serviceAccountName` in the Deployment
3. Update `subjects[0].name` in the RoleBinding/ClusterRoleBinding

## Testing RBAC

Verify permissions after deployment:

```bash
# Test namespace-restricted permissions
kubectl auth can-i get pods --as=system:serviceaccount:default:podboard
kubectl auth can-i delete pods --as=system:serviceaccount:default:podboard
kubectl auth can-i delete pods --as=system:serviceaccount:default:podboard -n kube-system

# Test cluster-wide permissions (if using cluster-wide RBAC)
kubectl auth can-i get pods --as=system:serviceaccount:default:podboard --all-namespaces
kubectl auth can-i delete pods --as=system:serviceaccount:default:podboard --all-namespaces
```