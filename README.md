# Secret Sync Operator

A Kubernetes operator that pulls secrets from an external API and syncs them as native Kubernetes Secrets, making them available for injection into pods.

---

## How It Works

1. You deploy the operator once with your API credentials (endpoint, access key, secret key)
2. Users create a simple `SecretSync` resource with just the secret name
3. The operator fetches the secret from the API and creates a Kubernetes Secret
4. Pods reference that Kubernetes Secret to inject values as environment variables or mounted files

```
User applies SecretSync  →  Operator fetches from API  →  K8s Secret created  →  Pod injects secret
```

---

## Prerequisites

- Kubernetes cluster (v1.26+)
- `kubectl` configured and connected to the cluster
- Docker (for building the image)
- Go 1.22+ (for local development only)

---

## Project Structure

```
secret-operator/
├── cmd/operator/main.go                        # Operator entrypoint
├── internal/
│   ├── api/v1alpha1/
│   │   ├── secretsync_types.go                 # CRD type definitions
│   │   ├── groupversion_info.go                # Scheme registration
│   │   └── zz_generated.deepcopy.go            # DeepCopy implementations
│   └── controller/
│       └── secretsync_controller.go            # Reconcile loop
├── config/
│   ├── crd/secretsyncs.yaml                    # CRD manifest
│   ├── rbac/rbac.yaml                          # ServiceAccount + ClusterRole
│   ├── manager/deployment.yaml                 # Operator deployment
│   └── examples/secretsync_example.yaml        # Example SecretSync resources
├── Dockerfile
└── go.mod
```

---

## Deployment

### Step 1 — Build and push the image

```bash
docker build -t your-registry/secret-sync-operator:latest .
docker push your-registry/secret-sync-operator:latest
```

### Step 2 — Install the CRD

```bash
kubectl apply -f config/crd/secretsyncs.yaml

# Verify
kubectl get crd secretsyncs.sync.bugx.ir
```

### Step 3 — Set your API credentials

Edit `config/manager/deployment.yaml` and fill in your actual values:

```yaml
stringData:
  SECRET_API_ENDPOINT: "http://your-api/api/v1/secrets/access"
  SECRET_API_ACCESS_KEY: "ak_your_actual_key"
  SECRET_API_SECRET_KEY: "sk_your_actual_key"
```

### Step 4 — Apply RBAC and deploy the operator

```bash
kubectl apply -f config/rbac/rbac.yaml
kubectl apply -f config/manager/deployment.yaml

# Verify the operator is running
kubectl get pods -n secret-sync-system
```

---

## Usage

### Create a SecretSync resource

The simplest form — just provide the secret name:

```yaml
apiVersion: sync.bugx.ir/v1alpha1
kind: SecretSync
metadata:
  name: my-auth-secrets
  namespace: default
spec:
  secretName: authapi
```

```bash
kubectl apply -f secretsync.yaml
```

### Check sync status

```bash
kubectl get secretsync -n default

# NAME              SECRET   READY   LAST SYNC              AGE
# my-auth-secrets   authapi  true    2024-01-01T10:00:00Z   5m

kubectl describe secretsync my-auth-secrets
```

### Verify the Kubernetes Secret was created

```bash
kubectl get secret authapi -n default
kubectl get secret authapi -n default -o yaml
```

---

## Injecting Secrets into Pods

### Option A — All keys as environment variables

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  namespace: default
spec:
  containers:
    - name: app
      image: my-app:latest
      envFrom:
        - secretRef:
            name: authapi   # matches spec.secretName in your SecretSync
```

### Option B — Specific keys as environment variables

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  namespace: default
spec:
  containers:
    - name: app
      image: my-app:latest
      env:
        - name: DB_USERNAME
          valueFrom:
            secretKeyRef:
              name: authapi
              key: username
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: authapi
              key: password
```

### Option C — Mount as files

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  namespace: default
spec:
  containers:
    - name: app
      image: my-app:latest
      volumeMounts:
        - name: secret-vol
          mountPath: /etc/secrets
          readOnly: true
  volumes:
    - name: secret-vol
      secret:
        secretName: authapi
```

### Verify injection

```bash
# Check env vars are present inside the container
kubectl exec my-app -- env | grep -i db
```

> **Note:** Make sure the SecretSync shows `READY=true` before starting the pod, otherwise the pod will fail with `secret not found`.

---

## SecretSync Full Spec Reference

```yaml
apiVersion: sync.bugx.ir/v1alpha1
kind: SecretSync
metadata:
  name: example
  namespace: default
spec:
  # Required: name of the secret to fetch from the external API
  secretName: authapi

  # Optional: namespace to create the K8s secret in
  # Defaults to the same namespace as this SecretSync
  targetNamespace: default

  # Optional: name of the K8s secret to create
  # Defaults to the value of secretName
  targetSecretName: authapi

  # Optional: how often to re-sync the secret
  # Defaults to 1h. Accepts: "30m", "1h", "24h", etc.
  refreshInterval: "1h"
```

---

## Local Development (no Docker required)

```bash
# Install the CRD on your cluster
kubectl apply -f config/crd/secretsyncs.yaml

# Set API credentials as environment variables
export SECRET_API_ENDPOINT="http://localhost:8080/api/v1/secrets/access"
export SECRET_API_ACCESS_KEY="ak_your_key"
export SECRET_API_SECRET_KEY="sk_your_key"

# Run the operator locally against your kubeconfig
go run ./cmd/operator/main.go
```

---

## Environment Variables

These are set on the operator deployment and are not exposed to users.

| Variable | Description | Required |
|---|---|---|
| `SECRET_API_ENDPOINT` | Full URL of the secret API | Yes |
| `SECRET_API_ACCESS_KEY` | API access key | Yes |
| `SECRET_API_SECRET_KEY` | API secret key | Yes |

---

## Troubleshooting

**Pod fails with `secret not found`**
Check the SecretSync is Ready before applying the pod:
```bash
kubectl get secretsync -n default
```

**Operator not starting**
Check the operator pod logs:
```bash
kubectl logs -n secret-sync-system deployment/secret-sync-operator
```

**Secret not updating**
Force a re-sync by annotating the SecretSync:
```bash
kubectl annotate secretsync my-auth-secrets force-sync=$(date +%s) --overwrite
```

**Check operator events**
```bash
kubectl describe secretsync my-auth-secrets
```