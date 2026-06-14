package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	syncv1alpha1 "github.com/yourorg/secret-operator/internal/api/v1alpha1"
)

// APIConfig holds the built-in configuration loaded from environment variables.
// Users never need to specify these in their SecretSync CRD.
type APIConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
}

// SecretAccessRequest is the request body sent to the secret API
type SecretAccessRequest struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Name      string `json:"name"`
}

// SecretAccessResponse is what the secret API returns
type SecretAccessResponse struct {
	Data        map[string]string `json:"data"`
	Description string            `json:"description"`
	Name        string            `json:"name"`
}

// SecretSyncReconciler reconciles a SecretSync object
type SecretSyncReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	APIConfig APIConfig
}

// LoadAPIConfig reads the built-in API config from environment variables.
// These are set once at operator deployment time (e.g. via a Secret + envFrom).
func LoadAPIConfig() (APIConfig, error) {
	endpoint := os.Getenv("SECRET_API_ENDPOINT")
	accessKey := os.Getenv("SECRET_API_ACCESS_KEY")
	secretKey := os.Getenv("SECRET_API_SECRET_KEY")

	if endpoint == "" {
		return APIConfig{}, fmt.Errorf("SECRET_API_ENDPOINT environment variable is required")
	}
	if accessKey == "" {
		return APIConfig{}, fmt.Errorf("SECRET_API_ACCESS_KEY environment variable is required")
	}
	if secretKey == "" {
		return APIConfig{}, fmt.Errorf("SECRET_API_SECRET_KEY environment variable is required")
	}

	return APIConfig{
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
	}, nil
}

// +kubebuilder:rbac:groups=sync.yourorg.io,resources=secretsyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sync.yourorg.io,resources=secretsyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

func (r *SecretSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the SecretSync resource
	secretSync := &syncv1alpha1.SecretSync{}
	if err := r.Get(ctx, req.NamespacedName, secretSync); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch SecretSync: %w", err)
	}

	logger.Info("Reconciling SecretSync", "name", secretSync.Name, "secretName", secretSync.Spec.SecretName)

	// Resolve target namespace and secret name
	targetNamespace := secretSync.Spec.TargetNamespace
	if targetNamespace == "" {
		targetNamespace = secretSync.Namespace
	}
	targetSecretName := secretSync.Spec.TargetSecretName
	if targetSecretName == "" {
		targetSecretName = secretSync.Spec.SecretName
	}

	// Fetch secrets from the external API
	apiResponse, err := r.fetchFromAPI(secretSync.Spec.SecretName)
	if err != nil {
		logger.Error(err, "Failed to fetch secret from API")
		return r.setStatus(ctx, secretSync, false, fmt.Sprintf("API fetch failed: %v", err), 30*time.Second)
	}

	// Create or update the Kubernetes Secret
	if err := r.upsertSecret(ctx, targetNamespace, targetSecretName, secretSync, apiResponse); err != nil {
		logger.Error(err, "Failed to upsert K8s secret")
		return r.setStatus(ctx, secretSync, false, fmt.Sprintf("K8s secret upsert failed: %v", err), 30*time.Second)
	}

	logger.Info("Successfully synced secret",
		"apiSecretName", secretSync.Spec.SecretName,
		"k8sSecretName", targetSecretName,
		"namespace", targetNamespace,
	)

	// Parse refresh interval and requeue
	refreshInterval, err := time.ParseDuration(secretSync.Spec.RefreshInterval)
	if err != nil {
		refreshInterval = time.Hour
	}

	return r.setStatus(ctx, secretSync, true,
		fmt.Sprintf("Synced successfully. Next refresh in %s", refreshInterval),
		refreshInterval,
	)
}

// fetchFromAPI calls the external secret API to retrieve secret data
func (r *SecretSyncReconciler) fetchFromAPI(secretName string) (*SecretAccessResponse, error) {
	reqBody := SecretAccessRequest{
		AccessKey: r.APIConfig.AccessKey,
		SecretKey: r.APIConfig.SecretKey,
		Name:      secretName,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(r.APIConfig.Endpoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to call API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResponse SecretAccessResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &apiResponse, nil
}

// upsertSecret creates or updates a Kubernetes Secret with the fetched data.
// The secret is labeled with the owning SecretSync so it can be tracked.
func (r *SecretSyncReconciler) upsertSecret(
	ctx context.Context,
	namespace, name string,
	owner *syncv1alpha1.SecretSync,
	apiResponse *SecretAccessResponse,
) error {
	// Convert string map to []byte map
	data := make(map[string][]byte)
	for k, v := range apiResponse.Data {
		data[k] = []byte(v)
	}

	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":          "secret-sync-operator",
				"secretsync.yourorg.io/owner-name":      owner.Name,
				"secretsync.yourorg.io/owner-namespace": owner.Namespace,
			},
			Annotations: map[string]string{
				"secretsync.yourorg.io/api-secret-name": apiResponse.Name,
				"secretsync.yourorg.io/description":     apiResponse.Description,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	existing := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return fmt.Errorf("failed to get existing secret: %w", err)
	}

	// Update in place
	existing.Data = desired.Data
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	return r.Update(ctx, existing)
}

// setStatus updates the SecretSync status subresource and returns a requeue result
func (r *SecretSyncReconciler) setStatus(
	ctx context.Context,
	secretSync *syncv1alpha1.SecretSync,
	ready bool,
	message string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	now := metav1.Now()
	secretSync.Status.Ready = ready
	secretSync.Status.Message = message
	secretSync.Status.ObservedGeneration = secretSync.Generation
	if ready {
		secretSync.Status.LastSyncTime = &now
	}

	if err := r.Status().Update(ctx, secretSync); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager registers the controller with the manager
func (r *SecretSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&syncv1alpha1.SecretSync{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
