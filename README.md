# ca-signer

A small HTTPS service that signs X.509 CSRs using a Smallstep CA. It exposes:
- GET /healthz — basic health check
- POST /sign — accepts a CSR and returns a signed certificate from the CA

The service bootstraps a Smallstep CA provisioner on startup, then listens on port 4443 with TLS enabled.


## Requirements
- Go (for local build/run)
- Access to a Smallstep CA and a provisioner that can mint certificates
- A root CA certificate file (PEM) for TLS verification
- The provisioner password (for the chosen provisioner)


## Configuration
Provide a YAML config file and pass its path as the first argument to the binary. Available fields:

- caURL: URL of the Smallstep CA (required)
- rootCAPath: path to the CA root certificate file (optional; defaults to the Smallstep default via pki.GetRootCAPath())
- provisionerPasswordFile: path to a file containing the provisioner password (optional; defaults to /home/step/password)
- address: address for the HTTP server to bind (optional; default ":4443")
- service: service name used when generating the bootstrap token (optional; default "ca-signer.default.svc")
- logFormat: "json" or "text" (optional)

Examples:
- example_config.yaml (for local runs)
- docker_config.yaml (volume-mount friendly example for container runs)


## Environment variables
- PROVISIONER_NAME: name of the provisioner to use (required; Docker image default is "autocert")
- PROVISIONER_KID: key ID for the provisioner (optional)


## Build and run

### Run locally (Go)
1) Prepare a config file (see example_config.yaml) and ensure the following exist:
   - root CA file at rootCAPath
   - provisioner password file at provisionerPasswordFile
   - valid PROVISIONER_NAME in your environment

2) Run:

```bash
export PROVISIONER_NAME=<your-provisioner>

go run . example_config.yaml
```

Or build and run:

```bash
go build -o ca-signer .
./ca-signer example_config.yaml
```

The server listens on the configured address (default :4443) and serves TLS.


### Run with Docker
Build the image:

```bash
docker build -t ca-signer .
```

Run (example using docker_config.yaml and mounted secrets/certs):

```bash
docker run --rm \
  --name ca-signer \
  -e PROVISIONER_NAME=<your-provisioner> \
  -e PROVISIONER_KID=<your-provisioner-kid> \
  -v $PWD/docker_config.yaml:/etc/ca-signer/config.yaml \
  -v $PWD/provisioner-password.txt:/provisioner-password.txt \
  -v $PWD/examples/root.crt:/etc/ca-signer/root.crt \
  -p 4443:4443 \
  ca-signer
```

Notes:
- docker_config.yaml expects:
  - caURL: https URL of your CA (e.g., https://ca-prod.fyve-system.svc.cluster.local)
  - rootCAPath: /etc/ca-signer/root.crt (as mounted above)
  - provisionerPasswordFile: /provisioner-password.txt (as mounted above)


## API

- GET /healthz
  - Returns 200 OK with body "ok" when healthy.

- POST /sign
  - Content-Type: application/json
  - Body:
    {
      "csr": <api.CertificateRequest JSON representation>,
      "notAfter": "<duration>"  // optional, e.g. "1h"
    }
  - Returns 201 Created with Smallstep api.SignResponse JSON on success.
  - mTLS is required by the default example client; ensure your client trusts the service certificate and presents a valid client cert if configured that way in your environment.

Because the JSON representation of api.CertificateRequest is non-trivial, use the provided example client or Smallstep libraries to construct requests.


## Example client
There is a runnable example in examples/client.go that:
- Generates a CSR in code
- Calls POST /sign over TLS

Prepare example material (using step CLI):

```bash
cd examples
# Obtain a client certificate and key that can call the signer:
step ca certificate example.com client.crt client.key --provisioner oidc
# Fetch the CA root used by the signer service:
step ca root root.crt

# Run the example client
go run client.go
```

The client posts to https://127.0.0.1:4443/sign by default and prints the JSON response.


## Files
- main.go — server implementation
- Dockerfile — multi-stage build for the server binary
- example_config.yaml — sample local config
- docker_config.yaml — sample container config and example docker run comment
- examples/client.go — example client for /sign
- examples/*.crt, *.key — example materials for the client
- provisioner-password.txt — example password file placeholder


## Logging
Set logFormat in the config to "json" or "text". Logs are written to stdout.
