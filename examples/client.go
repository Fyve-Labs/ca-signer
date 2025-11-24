package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"github.com/smallstep/certificates/api"
)

type SignRequest struct {
	CsrPEM   api.CertificateRequest `json:"csr"`
	NotAfter api.TimeDuration       `json:"notAfter"`
}

/*
*
Run test:
Assuming ca-signer running on 127.0.0.1:4443

$ cd examples
$ step ca certificate example.com client.crt client.key --provisioner oidc
$ step ca root root.crt
go run client.go
*/
func main() {
	subject := "example.com"
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}

	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: subject,
		},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}

	csr, err := x509.CreateCertificateRequest(rand.Reader, template, privateKey)
	if err != nil {
		log.Fatal(err)
	}
	cr, err := x509.ParseCertificateRequest(csr)
	if err != nil {
		log.Fatal(err)
	}

	if err := cr.CheckSignature(); err != nil {
		log.Fatal(err)
	}

	notAfter, _ := api.ParseTimeDuration("1h")
	signReq := &SignRequest{
		CsrPEM:   api.CertificateRequest{CertificateRequest: cr},
		NotAfter: notAfter,
	}

	resp, err := sign(signReq)
	if err != nil {
		log.Fatal(err)
	}

	jsonString, _ := json.Marshal(resp)
	log.Println(string(jsonString))
}

func sign(req *SignRequest) (*api.SignResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling request")
	}

	clientCert, err := tls.LoadX509KeyPair("client.crt", "client.key")
	if err != nil {
		return nil, errors.Wrap(err, "error loading client certificate")
	}

	caCert, err := os.ReadFile("root.crt")
	if err != nil {
		return nil, errors.Wrap(err, "error reading CA certificate")
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      caCertPool,
		},
	}

	client := &http.Client{Transport: transport}

	resp, err := client.Post("https://127.0.0.1:4443/sign", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, errors.Wrap(err, "error making request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var signResp api.SignResponse
	if err := json.NewDecoder(resp.Body).Decode(&signResp); err != nil {
		return nil, errors.Wrap(err, "error decoding response")
	}

	return &signResp, nil
}
