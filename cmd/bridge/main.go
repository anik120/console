package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/coreos/go-oidc/oidc"
	"github.com/coreos/pkg/capnslog"
	"github.com/coreos/pkg/flagutil"
	"k8s.io/kubernetes/pkg/client/restclient"

	"github.com/coreos-inc/bridge/auth"
	"github.com/coreos-inc/bridge/server"
	"github.com/coreos-inc/bridge/stats"
)

var (
	log = capnslog.NewPackageLogger("github.com/coreos-inc/bridge", "cmd/main")
)

func main() {
	rl := capnslog.MustRepoLogger("github.com/coreos-inc/bridge")
	capnslog.SetFormatter(capnslog.NewStringFormatter(os.Stderr))

	fs := flag.NewFlagSet("bridge", flag.ExitOnError)
	fs.String("listen", "http://0.0.0.0:9000", "")
	logLevel := fs.String("log-level", "", "level of logging information by package (pkg=level)")
	publicDir := fs.String("public-dir", "./frontend/public", "directory containing static web assets")
	k8sInCluster := fs.Bool("k8s-in-cluster", false, "Configure --k8s-endpoint, --k8s-bearer-token and TLS configuration for communication with Kubernetes API Server from environment, typically used when deploying as a Kubernetes pod")
	fs.String("k8s-endpoint", "https://172.17.4.101:29101", "URL of the Kubernetes API server, ignored when --k8s-in-cluster=true")
	k8sBearerToken := fs.String("k8s-bearer-token", "", "Authorization token to send with proxied Kubernetes API requests. This should only be used when --disable-auth=true, as any OIDC-related authorization information will be blindly overridden. This flag is ignored with --k8s-in-cluster=true")
	fs.String("host", "http://127.0.0.1:9000", "The externally visible hostname/port of the service. Used in OIDC/OAuth2 Redirect URL.")
	disableAuth := fs.Bool("disable-auth", false, "Disable all forms of authentication.")
	authClientID := fs.String("auth-client-id", "", "The OIDC OAuth2 Client ID.")
	authClientSecret := fs.String("auth-client-secret", "", "The OIDC/OAuth2 Client Secret.")
	fs.String("auth-issuer-url", "", "The OIDC/OAuth2 issuer URL")
	enableDexUserManagement := fs.Bool("enable-dex-user-management", false, "Use auth-issuer-url as an endpoint for dex's user managment API.")
	tlsCertFile := fs.String("tls-cert-file", "", "TLS certificate. If the certificate is signed by a certificate authority, the certFile should be the concatenation of the server's certificate followed by the CA's certificate.")
	tlsKeyFile := fs.String("tls-key-file", "", "The TLS certificate key.")
	caFile := fs.String("ca-file", "", "PEM File containing trusted certificates of trusted CAs. If not present, the system's Root CAs will be used.")
	insecureSkipVerifyK8sCA := fs.Bool("insecure-skip-verify-k8s-tls", false, "DEV ONLY. When true, skip verification of certs presented by k8s API server. This is ignored when -k8s-in-cluster is set.")
	tectonicVersion := fs.String("tectonic-version", "UNKNOWN", "The current tectonic system version, served at /version")
	idFile := fs.String("identity-file", "", "A file that identifies the console owner")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if err := flagutil.SetFlagsFromEnv(fs, "BRIDGE"); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	capnslog.SetGlobalLogLevel(capnslog.INFO)
	if *logLevel != "" {
		llc, err := rl.ParseLogLevelConfig(*logLevel)
		if err != nil {
			log.Fatal(err)
		}
		rl.SetLogLevel(llc)
		log.Infof("Setting log level to %s", *logLevel)
	}

	if *idFile != "" {
		go stats.GenerateStats(*idFile)
	}

	certPool, err := newCertPool(*caFile)
	if err != nil {
		log.Fatalf("could not initialize CA certificate pool: %v", err)
	}

	k8sURL := validateURLFlag(fs, "k8s-endpoint")

	var kCfg *server.ProxyConfig
	if *k8sInCluster {
		cc, err := restclient.InClusterConfig()
		if err != nil {
			log.Fatalf("Error inferring Kubernetes config from environment: %v", err)
		}

		inClusterTLSCfg, err := restclient.TLSConfigFor(cc)
		if err != nil {
			log.Fatalf("Error creating TLS config from Kubernetes config: %v", err)
		}

		k8sURL, err := url.Parse(cc.Host)
		if err != nil {
			log.Fatalf("Kubernetes config provided invalid URL: %v", err)
		}

		kCfg = &server.ProxyConfig{
			TLSClientConfig: inClusterTLSCfg,
			HeaderBlacklist: []string{"Cookie"},
			Endpoint:        k8sURL,
			TokenExtractor:  server.ConstantTokenExtractor(cc.BearerToken),
		}

	} else {
		kCfg = &server.ProxyConfig{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: *insecureSkipVerifyK8sCA,
			},
			HeaderBlacklist: []string{"Cookie"},
			Endpoint:        k8sURL,
			TokenExtractor:  server.ConstantTokenExtractor(*k8sBearerToken),
		}
	}

	var dexCfg *server.ProxyConfig
	if *enableDexUserManagement {
		flag := fs.Lookup("auth-issuer-url")
		if flag.Value.String() == "" {
			log.Fatalf("auth-issuer-url is required when using enable-dex-user-management")
		}
		if *disableAuth {
			log.Fatalf("enable-dex-user-management requires authentication enabled")
		}
		dexURL := validateURLFlag(fs, "auth-issuer-url")
		dexURL.Path = "/api"
		dexCfg = &server.ProxyConfig{
			Endpoint: dexURL,
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
			HeaderBlacklist: []string{"Cookie"},
			TokenExtractor:  auth.ExtractTokenFromCookie,
		}
	}

	srv := &server.Server{
		K8sProxyConfig:  kCfg,
		DexProxyConfig:  dexCfg,
		TectonicVersion: *tectonicVersion,
		PublicDir:       *publicDir,
	}

	srv.NewUserAuthCallbackURL = validateURLFlag(fs, "host")
	srv.NewUserAuthCallbackURL.Path = server.AuthSuccessURL

	authLoginCallbackURL := validateURLFlag(fs, "host")
	authLoginCallbackURL.Path = server.AuthLoginCallbackEndpoint

	if *disableAuth {
		log.Warningf("running with AUTHENTICATION DISABLED!")
	} else {
		validateFlagNotEmpty(fs, "auth-client-id")
		validateFlagNotEmpty(fs, "auth-client-secret")
		iURL := validateURLFlag(fs, "auth-issuer-url")

		ocfg := oidc.ClientConfig{
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs: certPool,
					},
				},
				Timeout: time.Second * 5,
			},
			Credentials: oidc.ClientCredentials{
				ID:     *authClientID,
				Secret: *authClientSecret,
			},
			RedirectURL: authLoginCallbackURL.String(),
		}

		auther, err := auth.NewAuthenticator(ocfg, iURL, server.AuthErrorURL, server.AuthSuccessURL)
		if err != nil {
			log.Fatalf("Error initializing authenticator: %v", err)
		}
		auther.Start()
		srv.Auther = auther
	}

	lu := validateURLFlag(fs, "listen")
	switch lu.Scheme {
	case "http":
	case "https":
		if *tlsCertFile == "" || *tlsKeyFile == "" {
			log.Fatalf("Must provide certificate file and private key file")
		}
	default:
		log.Fatalf("Only 'http' and 'https' schemes are supported")
	}

	httpsrv := &http.Server{
		Addr:    lu.Host,
		Handler: srv.HTTPHandler(),
	}

	log.Infof("Binding to %s...", httpsrv.Addr)
	if lu.Scheme == "https" {
		log.Info("using TLS")
		log.Fatal(httpsrv.ListenAndServeTLS(*tlsCertFile, *tlsKeyFile))
	} else {
		log.Info("not using TLS")
		log.Fatal(httpsrv.ListenAndServe())
	}
}

func validateURLFlag(fs *flag.FlagSet, name string) *url.URL {
	validateFlagNotEmpty(fs, name)
	flag := fs.Lookup(name)
	ur, err := url.Parse(flag.Value.String())
	if err != nil {
		log.Fatalf("Invalid flag: %s, error: %v", flag.Name, err)
	}
	if ur == nil || ur.String() == "" {
		log.Fatalf("Missing required flag: %s", flag.Name)
	}
	if ur.Scheme == "" || ur.Host == "" {
		log.Fatalf("Invalid flag: %s, error: malformed URL", flag.Name)
	}
	return ur
}

func validateFlagNotEmpty(fs *flag.FlagSet, name string) {
	flag := fs.Lookup(name)
	if flag.Value.String() == "" {
		log.Fatalf("Missing required flag: %s", flag.Name)
	}
}

func newCertPool(certFile string) (*x509.CertPool, error) {
	if certFile == "" {
		return nil, nil
	}
	certPool := x509.NewCertPool()

	pemByte, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}

	ok := certPool.AppendCertsFromPEM(pemByte)
	if !ok {
		return nil, errors.New("Could not parse CA File.")
	}

	return certPool, nil
}
