package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Config struct {
	API struct {
		Endpoint   string `yaml:"endpoint"`
		AccessKey  string `yaml:"accessKey"`
		SecretKey  string `yaml:"secretKey"`
		SecretName string `yaml:"secretName"`
	} `yaml:"api"`
	Kubernetes struct {
		Namespace  string `yaml:"namespace"`
		SecretName string `yaml:"secretName"`
	} `yaml:"kubernetes"`
}

type SecretAccessRequest struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Name      string `json:"name"`
}

type SecretAccessResponse struct {
	Data        map[string]string `json:"data"`
	Description string            `json:"description"`
	Name        string            `json:"name"`
}

func loadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func fetchSecrets(config *Config) (*SecretAccessResponse, error) {
	reqBody := SecretAccessRequest{
		AccessKey: config.API.AccessKey,
		SecretKey: config.API.SecretKey,
		Name:      config.API.SecretName,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(config.API.Endpoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to make API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResponse SecretAccessResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &apiResponse, nil
}

func getKubernetesClient() (*kubernetes.Clientset, error) {
	var kubeconfig string
	if home := homeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	// Try to use in-cluster config first (if running in a pod)
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig file
		if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
			return nil, fmt.Errorf("kubeconfig file not found at %s and not running in cluster", kubeconfig)
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func createKubernetesSecret(clientset *kubernetes.Clientset, config *Config, secretData *SecretAccessResponse) error {
	ctx := context.Background()
	namespace := config.Kubernetes.Namespace
	secretName := config.Kubernetes.SecretName
	if secretName == "" {
		secretName = secretData.Name
	}

	// Convert string map to []byte map for Kubernetes secret
	data := make(map[string][]byte)
	for k, v := range secretData.Data {
		data[k] = []byte(v)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: data,
		Type: corev1.SecretTypeOpaque,
	}

	// Check if secret already exists
	_, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check if secret exists: %w", err)
	}

	if err == nil {
		// Secret exists, update it
		_, err = clientset.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
		log.Printf("Successfully updated secret '%s' in namespace '%s'", secretName, namespace)
	} else {
		// Secret doesn't exist, create it
		_, err = clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
		log.Printf("Successfully created secret '%s' in namespace '%s'", secretName, namespace)
	}

	return nil
}

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	log.Println("Loading configuration...")
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Println("Fetching secrets from API...")
	secretData, err := fetchSecrets(config)
	if err != nil {
		log.Fatalf("Failed to fetch secrets: %v", err)
	}

	log.Printf("Retrieved secret '%s' with %d keys", secretData.Name, len(secretData.Data))

	log.Println("Connecting to Kubernetes...")
	clientset, err := getKubernetesClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	log.Println("Creating/updating Kubernetes secret...")
	if err := createKubernetesSecret(clientset, config, secretData); err != nil {
		log.Fatalf("Failed to create/update secret: %v", err)
	}

	log.Println("Done!")
}
