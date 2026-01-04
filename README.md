# Secret Agent

A Go application that fetches secrets from an API and creates/updates Kubernetes secrets.

## Features

- Fetches secrets from a REST API endpoint
- Creates or updates Kubernetes secrets in a specified namespace
- All configuration managed via YAML file
- Supports both in-cluster and local kubeconfig authentication

## Configuration

Edit `config.yaml` to configure:

- API endpoint and credentials
- Kubernetes namespace and secret name

## Usage

1. Install dependencies:
```bash
go mod download
```

2. Update `config.yaml` with your settings

3. Run the application:
```bash
go run main.go
```

Or specify a custom config file:
```bash
go run main.go /path/to/config.yaml
```

## Build

```bash
go build -o secret_agent
./secret_agent
```

## Requirements

- Go 1.21 or later
- Kubernetes cluster access (via kubeconfig or in-cluster config)
- Valid API credentials

