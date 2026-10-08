package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	out := flag.String("out", `certs`, "certificate output directory (relative to the current directory; run from client/)")
	dnsList := flag.String("dns", "localhost,monitor.company.example", "comma-separated DNS SANs for the server cert")
	ipList := flag.String("ip", "127.0.0.1,::1", "comma-separated IP SANs for the server cert")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o700); err != nil {
		panic(err)
	}
	now := time.Now().UTC()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	caTemplate := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "Monitor Local Test CA", Organization: []string{"Monitor Local Test"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	must(err)
	writeCert(filepath.Join(*out, "ca.pem"), caDER)
	writeKey(filepath.Join(*out, "ca-key.pem"), caKey)

	createLeaf(*out, "server", caTemplate, caDER, caKey, false, splitCSV(*dnsList), parseIPs(*ipList), now)
	createLeaf(*out, "client", caTemplate, caDER, caKey, true, nil, nil, now)

	fmt.Printf("generated certificates in %s\n", *out)
	fmt.Printf("  server SAN DNS=%s IP=%s\n", *dnsList, *ipList)
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseIPs(raw string) []net.IP {
	var out []net.IP
	for _, part := range splitCSV(raw) {
		ip := net.ParseIP(part)
		if ip == nil {
			panic("invalid IP SAN: " + part)
		}
		out = append(out, ip)
	}
	return out
}

func createLeaf(out, name string, ca *x509.Certificate, caDER []byte, caKey *ecdsa.PrivateKey, client bool, dns []string, ips []net.IP, now time.Time) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	usage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	commonName := "Monitor Local Test Server"
	if client {
		usage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		commonName = "monitor-agent"
	}
	template := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Monitor Local Test"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           usage,
		DNSNames:              dns,
		IPAddresses:           ips,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	must(err)
	writeCert(filepath.Join(out, name+".pem"), der)
	writeKey(filepath.Join(out, name+"-key.pem"), key)
}

func randomSerial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	must(err)
	return n
}

func writeCert(path string, der []byte) {
	writeFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func writeKey(path string, key *ecdsa.PrivateKey) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	must(err)
	writeFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
}

func writeFile(path string, data []byte, mode os.FileMode) {
	if err := os.WriteFile(path, data, mode); err != nil {
		panic(err)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
