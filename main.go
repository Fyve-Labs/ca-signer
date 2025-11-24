package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
	"unicode"

	log "github.com/sirupsen/logrus"
	"github.com/smallstep/certificates/api"
	"github.com/smallstep/certificates/api/render"
	"github.com/smallstep/certificates/ca"
	"github.com/smallstep/certificates/errs"
	"sigs.k8s.io/yaml"
)

type Config struct {
	CaURL                   string `yaml:"caURL"`
	RootCAPath              string `yaml:"rootCAPath"`
	ProvisionerPasswordFile string `yaml:"provisionerPasswordFile"`
	Address                 string `yaml:"address"`
	Service                 string `yaml:"service"`
	LogFormat               string `yaml:"logFormat"`
}

type SignRequest struct {
	CsrPEM   api.CertificateRequest `json:"csr"`
	NotAfter api.TimeDuration       `json:"notAfter"`
}

func (s *SignRequest) Validate() error {
	if s.CsrPEM.CertificateRequest == nil {
		return errs.BadRequest("missing csr")
	}

	if err := s.CsrPEM.CertificateRequest.CheckSignature(); err != nil {
		return errs.BadRequestErr(err, "invalid csr")
	}

	return nil
}

// GetAddress returns the address set in the configuration, defaults to ":4443"
// if it's not specified.
func (c Config) GetAddress() string {
	if c.Address != "" {
		return c.Address
	}

	return ":4443"
}

func (c Config) GetServiceName() string {
	if c.Service != "" {
		return c.Service
	}

	return "ca-signer.step.svc.cluster.local"
}

func (c Config) GetRootCAPath() string {
	if c.RootCAPath != "" {
		return c.RootCAPath
	}

	return "/home/step/certs/root_ca.crt"
}

// GetProvisionerPasswordPath returns the path to the provisioner password,
// defaults to "/home/step/password" if not specified in the
// configuration.
func (c Config) GetProvisionerPasswordPath() string {
	if c.ProvisionerPasswordFile != "" {
		return c.ProvisionerPasswordFile
	}

	return "/home/step/password/password"
}

func main() {
	config, err := loadConfig(os.Args[1])
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)
	if config.LogFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	}
	if config.LogFormat == "text" {
		log.SetFormatter(&log.TextFormatter{})
	}

	log.WithFields(log.Fields{
		"config": config,
	}).Info("Loaded config")

	provisionerName := os.Getenv("PROVISIONER_NAME")
	provisionerKid := os.Getenv("PROVISIONER_KID")
	log.WithFields(log.Fields{
		"provisionerName": provisionerName,
		"provisionerKid":  provisionerKid,
	}).Info("Loaded provisioner configuration")

	password, err := readPasswordFromFile(config.GetProvisionerPasswordPath())
	if err != nil {
		panic(err)
	}

	provisioner, err := ca.NewProvisioner(
		provisionerName, provisionerKid, config.CaURL, password,
		ca.WithRootFile(config.GetRootCAPath()))
	if err != nil {
		log.Errorf("Error loading provisioner: %v", err)
		os.Exit(1)
	}
	log.WithFields(log.Fields{
		"name": provisioner.Name(),
		"kid":  provisioner.Kid(),
	}).Info("Loaded provisioner")

	token, err := provisioner.Token(config.GetServiceName(), config.GetServiceName(), "127.0.0.1")
	if err != nil {
		log.WithField("error", err).Errorf("Error generating bootstrap token during signer startup")
		os.Exit(1)
	}
	log.WithField("name", config.GetServiceName()).Infof("Generated bootstrap token for signer")
	// make sure to cancel the renew goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := ca.BootstrapServer(ctx, token, &http.Server{
		Addr:              config.GetAddress(),
		ReadHeaderTimeout: 15 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/sign" {
				log.WithField("path", r.URL.Path).Error("Bad Request: 404 Not Found")
				http.NotFound(w, r)
				return
			}

			var request SignRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				render.Error(w, r, errs.BadRequestErr(err, "error reading request body"))
				return
			}

			if err := request.Validate(); err != nil {
				render.Error(w, r, err)
				return
			}

			csr := request.CsrPEM
			sans := append([]string{}, csr.DNSNames...)
			sans = append(sans, csr.EmailAddresses...)
			for _, ip := range csr.IPAddresses {
				sans = append(sans, ip.String())
			}
			for _, u := range csr.URIs {
				sans = append(sans, u.String())
			}

			subject := csr.Subject.CommonName
			if subject == "" {
				subject = generateSubject(sans)
			}

			token, err := provisioner.Token(subject, sans...)
			if err != nil {
				render.Error(w, r, err)
				return
			}

			resp, err := provisioner.Sign(&api.SignRequest{
				CsrPEM:   request.CsrPEM,
				OTT:      token,
				NotAfter: request.NotAfter,
			})

			if err != nil {
				render.Error(w, r, err)
				return
			}

			render.JSONStatus(w, r, resp, http.StatusCreated)
		}),
	})

	if err != nil {
		panic(err)
	}

	log.Info("Listening on", config.GetAddress(), "...")
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		panic(err)
	}
}

func loadConfig(file string) (*Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// readPasswordFromFile reads and returns the password from the given filename.
// The contents of the file will be trimmed at the right.
func readPasswordFromFile(filename string) ([]byte, error) {
	password, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	password = bytes.TrimRightFunc(password, unicode.IsSpace)
	return password, nil
}

func generateSubject(sans []string) string {
	if len(sans) == 0 {
		return "127.0.0.1"
	}

	for _, s := range sans {
		if s != "127.0.0.1" && s != "localhost" {
			return s
		}
	}

	return sans[0]
}
