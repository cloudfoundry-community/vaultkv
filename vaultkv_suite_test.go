package vaultkv_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/cloudfoundry-community/vaultkv"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestVaultkv(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Vaultkv Suite")
}

var currentVaultProcess *os.Process
var processChan = make(chan *os.ProcessState)
var processOutputWriter, processOutputReader *os.File

var (
	vaultProcessLocation string
	configLocation       string
	certLocation         string
	keyLocation          string
)

var vaultURI *url.URL

var _ = BeforeSuite(func() {
	var err error

	vaultProcessLocation, err = exec.LookPath("vault")
	if err != nil {
		panic("vault was not found in your PATH")
	}

	const uriStr = "https://127.0.0.1:8201"
	vaultURI, err = url.Parse(uriStr)
	if err != nil {
		panic(fmt.Sprintf("Could not parse Vault URI: %s", uriStr))
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("Could not generate private key: %s", err))
	}

	templateCert := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Second),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true,
	}
	cert, err := x509.CreateCertificate(
		rand.Reader,
		&templateCert,
		&templateCert,
		privateKey.Public(),
		privateKey)
	if err != nil {
		panic(fmt.Sprintf("Could not generate certificate: %s", err))
	}

	certFile, err := ioutil.TempFile(os.TempDir(), "vaultkv-test-cert")
	if err != nil {
		panic(fmt.Sprintf("Could not make temp file for cert: %s", err))
	}
	certLocation = certFile.Name()

	err = pem.Encode(certFile, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	})
	if err != nil {
		panic(fmt.Sprintf("Could not write test certificate to file: %s", err))
	}

	keyFile, err := ioutil.TempFile(os.TempDir(), "vaultkv-test-key")
	if err != nil {
		panic(fmt.Sprintf("Could not make temp file for key: %s", err))
	}

	keyLocation = keyFile.Name()
	err = pem.Encode(keyFile, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	if err != nil {
		panic(fmt.Sprintf("Could not write test key to file: %s", err))
	}

	configFile, err := ioutil.TempFile(os.TempDir(), "vaultkv-test-config")
	if err != nil {
		panic(fmt.Sprintf("Could not make temp file for cert: %s", err))
	}
	configLocation = configFile.Name()
	var vaultConfig = fmt.Sprintf(`
storage "inmem" {}

disable_mlock = true

listener "tcp" {
  address = "%s"
  tls_cert_file = "%s"
  tls_key_file = "%s"
}
`, vaultURI.Host, certLocation, keyLocation)
	_, err = configFile.WriteString(vaultConfig)
	if err != nil {
		panic(fmt.Sprintf("Could not write test config to file: %s", err))
	}
})

var _ = AfterSuite(func() {
	if configLocation != "" {
		os.Remove(configLocation)
	}

	if keyLocation != "" {
		os.Remove(keyLocation)
	}

	if certLocation != "" {
		os.Remove(certLocation)
	}

	if currentVaultProcess != nil {
		StopVault()
	}
})

func StartVault() {
	if currentVaultProcess != nil {
		panic("Clean up your vault process")
	}

	var err error

	//Gotta get that IPC from Vault in case we want to report errors
	processOutputReader, processOutputWriter, err = os.Pipe()
	if err != nil {
		panic("Could not set up IPC file descriptors")
	}

	loggingBuffer := &bytes.Buffer{}

	go io.Copy(loggingBuffer, processOutputReader)
	defer func() {
		if currentVaultProcess == nil {
			io.Copy(GinkgoWriter, loggingBuffer)
		}
	}()

	process, err := os.StartProcess(
		vaultProcessLocation, []string{vaultProcessLocation, "server", "-config", configLocation},
		&os.ProcAttr{
			Files: []*os.File{
				nil,                 //STDIN
				processOutputWriter, //STDOUT
				processOutputWriter, //STDERR
			},
		},
	)
	if err != nil {
		panic(fmt.Sprintf("Could not start Vault process: %s", err))
	}

	go func() {
		pState, err := process.Wait()
		if err != nil {
			panic(fmt.Sprintf("Err encountered while waiting on vault process: %s", err))
		}

		processChan <- pState
	}()

	startTime := time.Now()
	nextWarning := 5 * time.Second
	everySoOften := time.NewTicker(100 * time.Millisecond)
	client := NewTestClient()

	for {
		select {
		case <-everySoOften.C:
			err = client.Health(true)
			if err != nil {
				if _, isUninitialized := err.(*vaultkv.ErrUninitialized); isUninitialized {
					goto getMeOuttaHere
				}
			}

			if time.Since(startTime) > nextWarning {
				fmt.Printf("Been waiting for Vault server to start for %d seconds...\n", int64(nextWarning/time.Second))
				fmt.Println(err)
				nextWarning += 1 * time.Second
			}

		case <-processChan:
			panic("Vault exited prematurely")
		}
	}
getMeOuttaHere:

	currentVaultProcess = process
	everySoOften.Stop()
}

func StopVault() {
	if currentVaultProcess == nil {
		panic("No vault process to end")
	}

	err := currentVaultProcess.Signal(os.Interrupt)
	if err != nil {
		panic(fmt.Sprintf("Could not send interrupt signal to Vault process: %s", err))
	}

	_ = <-processChan
	processOutputWriter.Close()
	processOutputReader.Close()

	currentVaultProcess = nil
}

func NewTestClient() *vaultkv.Client {
	return &vaultkv.Client{
		VaultURL: vaultURI,
		Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	}
}
