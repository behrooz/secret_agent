package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretSyncSpec defines the desired state of SecretSync
type SecretSyncSpec struct {
	// SecretName is the name of the secret to fetch from the API
	// +kubebuilder:validation:Required
	SecretName string `json:"secretName"`

	// TargetNamespace is where the K8s secret will be created.
	// Defaults to the same namespace as the SecretSync resource.
	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// TargetSecretName is the name of the K8s secret to create/update.
	// Defaults to the secretName value.
	// +optional
	TargetSecretName string `json:"targetSecretName,omitempty"`

	// RefreshInterval is how often to re-sync the secret (e.g. "1h", "30m").
	// Defaults to "1h".
	// +optional
	// +kubebuilder:default="1h"
	RefreshInterval string `json:"refreshInterval,omitempty"`
}

// SecretSyncStatus defines the observed state of SecretSync
type SecretSyncStatus struct {
	// Ready indicates whether the secret has been successfully synced
	Ready bool `json:"ready"`

	// LastSyncTime is the last time the secret was successfully fetched and applied
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Message provides human-readable info about the current state
	// +optional
	Message string `json:"message,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed SecretSync
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Secret",type=string,JSONPath=`.spec.secretName`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Last Sync",type=string,JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SecretSync is the Schema for the secretsyncs API
type SecretSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecretSyncSpec   `json:"spec,omitempty"`
	Status SecretSyncStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SecretSyncList contains a list of SecretSync
type SecretSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecretSync `json:"items"`
}
