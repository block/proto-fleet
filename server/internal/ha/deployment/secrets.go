package deployment

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"
)

const (
	etcdRootPasswordFile    = "etcd-root-password"    //nolint:gosec // Filename, not a credential.
	fleetEtcdPasswordFile   = "fleet-etcd-password"   //nolint:gosec // Filename, not a credential.
	patroniEtcdPasswordFile = "patroni-etcd-password" //nolint:gosec // Filename, not a credential.
	fleetEnvironmentFile    = "fleet.env"             //nolint:gosec // Filename, not a credential.
	certificateValidity     = 10 * 365 * 24 * time.Hour
)

var databasePasswordFiles = []string{
	patroniEtcdPasswordFile,
	"patroni-rest-password",
	"postgres-superuser-password",
	"postgres-replication-password",
	"fleet-db-password",
}

type certificateAuthority struct {
	certificate *x509.Certificate
	key         *rsa.PrivateKey
	certPEM     []byte
}

// GenerateSecrets creates the complete offline and per-host credential layout.
func GenerateSecrets(outputDir string, hostIPs [3]string, virtualIP string) (err error) {
	seen := make(map[netip.Addr]struct{}, len(hostIPs))
	addresses := make([]netip.Addr, len(hostIPs))
	for i, rawIP := range hostIPs {
		ip, parseErr := netip.ParseAddr(rawIP)
		if parseErr != nil || !ip.Is4() {
			return fmt.Errorf("invalid IPv4 address: %s", rawIP)
		}
		if _, duplicate := seen[ip]; duplicate {
			return fmt.Errorf("host IPv4 addresses must be unique")
		}
		seen[ip] = struct{}{}
		addresses[i] = ip
	}
	vip, ok := parseRoutableIPv4(virtualIP)
	if !ok {
		return fmt.Errorf("invalid virtual IPv4 address: %s", virtualIP)
	}
	if _, duplicate := seen[vip]; duplicate {
		return fmt.Errorf("virtual IPv4 address must differ from every host address")
	}

	if err := os.MkdirAll(filepath.Dir(outputDir), 0o700); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		return fmt.Errorf("output path already exists or cannot be created: %s: %w", outputDir, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(outputDir)
		}
	}()

	for _, name := range []string{"offline", "ha-a", "ha-b", "ha-c"} {
		if err := os.Mkdir(filepath.Join(outputDir, name), 0o700); err != nil {
			return fmt.Errorf("create %s secrets directory: %w", name, err)
		}
	}

	now := time.Now()
	ca, err := newCertificateAuthority(now)
	if err != nil {
		return err
	}
	offlineDir := filepath.Join(outputDir, "offline")
	if err := writeFile(filepath.Join(offlineDir, "service-ca.crt"), ca.certPEM, 0o644); err != nil {
		return err
	}
	passwordFiles := append([]string{etcdRootPasswordFile, fleetEtcdPasswordFile}, databasePasswordFiles...)
	for _, name := range passwordFiles {
		password, randomErr := randomHex(32)
		if randomErr != nil {
			return fmt.Errorf("generate %s: %w", name, randomErr)
		}
		if err := writeFile(filepath.Join(offlineDir, name), append(password, '\n'), 0o600); err != nil {
			return err
		}
	}
	fleetDatabasePassword, err := readPassword(filepath.Join(offlineDir, "fleet-db-password"))
	if err != nil {
		return fmt.Errorf("read generated Fleet database password: %w", err)
	}
	// Both Fleet hosts share application identity because they alternate against
	// the same database and encrypted state.
	authSecret, err := randomHex(32)
	if err != nil {
		return fmt.Errorf("generate Fleet authentication secret: %w", err)
	}
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		return fmt.Errorf("generate Fleet encryption master key: %w", err)
	}
	fleetEnvironment := []byte(fmt.Sprintf(
		"AUTH_CLIENT_SECRET_KEY=%s\nENCRYPT_SERVICE_MASTER_KEY=%s\nDB_DSN=postgresql://fleet:%s@%s:5432,%s:5432/fleet?target_session_attrs=read-write&sslmode=verify-full&sslrootcert=/etc/proto-fleet/ha/service-ca.crt\n",
		authSecret,
		base64.StdEncoding.EncodeToString(masterKey),
		fleetDatabasePassword,
		hostIPs[0],
		hostIPs[1],
	))
	if err := writeFile(filepath.Join(offlineDir, fleetEnvironmentFile), fleetEnvironment, 0o600); err != nil {
		return err
	}

	jwtKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate etcd JWT key: %w", err)
	}
	jwtPrivate, err := x509.MarshalPKCS8PrivateKey(jwtKey)
	if err != nil {
		return fmt.Errorf("encode etcd JWT private key: %w", err)
	}
	jwtPublic, err := x509.MarshalPKIXPublicKey(&jwtKey.PublicKey)
	if err != nil {
		return fmt.Errorf("encode etcd JWT public key: %w", err)
	}
	jwtPrivatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: jwtPrivate})
	jwtPublicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: jwtPublic})
	if err := writeFile(filepath.Join(offlineDir, "etcd-jwt.key"), jwtPrivatePEM, 0o600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(offlineDir, "etcd-jwt.pub"), jwtPublicPEM, 0o644); err != nil {
		return err
	}
	fleetEtcdPassword, err := os.ReadFile(filepath.Join(offlineDir, fleetEtcdPasswordFile))
	if err != nil {
		return fmt.Errorf("read offline %s: %w", fleetEtcdPasswordFile, err)
	}

	hosts := []struct {
		name     string
		address  netip.Addr
		database bool
	}{
		{name: "ha-a", address: addresses[0], database: true},
		{name: "ha-b", address: addresses[1], database: true},
		{name: "ha-c", address: addresses[2]},
	}
	for _, host := range hosts {
		nodeDir := filepath.Join(outputDir, host.name)
		if err := writeFile(filepath.Join(nodeDir, "service-ca.crt"), ca.certPEM, 0o644); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(nodeDir, "etcd-jwt.key"), jwtPrivatePEM, 0o600); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(nodeDir, "etcd-jwt.pub"), jwtPublicPEM, 0o644); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(nodeDir, fleetEtcdPasswordFile), fleetEtcdPassword, 0o600); err != nil {
			return err
		}
		if err := issueCertificate(nodeDir, "etcd-server", "etcd-"+host.name, host.address, ca, now, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}); err != nil {
			return err
		}
		if err := issueCertificate(nodeDir, "etcd-peer", "etcd-peer-"+host.name, host.address, ca, now, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}); err != nil {
			return err
		}
		if !host.database {
			continue
		}
		if err := issueCertificate(nodeDir, "patroni-rest", "patroni-"+host.name, host.address, ca, now, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}); err != nil {
			return err
		}
		if err := issueCertificate(nodeDir, "postgres", "postgres-"+host.name, host.address, ca, now, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}); err != nil {
			return err
		}
		// Each host gets its own keypair, but both certificates identify the VIP.
		if err := issueCertificate(nodeDir, "fleet-client", "fleet-client-"+host.name, vip, ca, now, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(nodeDir, fleetEnvironmentFile), fleetEnvironment, 0o600); err != nil {
			return err
		}
		for _, name := range databasePasswordFiles {
			contents, readErr := os.ReadFile(filepath.Join(offlineDir, name))
			if readErr != nil {
				return fmt.Errorf("read offline %s: %w", name, readErr)
			}
			if err := writeFile(filepath.Join(nodeDir, name), contents, 0o600); err != nil {
				return err
			}
		}
	}

	return nil
}

func newCertificateAuthority(now time.Time) (certificateAuthority, error) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return certificateAuthority{}, fmt.Errorf("generate service CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return certificateAuthority{}, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Proto Fleet HA Root CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(certificateValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return certificateAuthority{}, fmt.Errorf("create service CA certificate: %w", err)
	}
	return certificateAuthority{
		certificate: template,
		key:         key,
		certPEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}, nil
}

func issueCertificate(dir, name, commonName string, address netip.Addr, ca certificateAuthority, now time.Time, usages []x509.ExtKeyUsage) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate %s key: %w", name, err)
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(certificateValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IP(address.AsSlice())},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.certificate, &key.PublicKey, ca.key)
	if err != nil {
		return fmt.Errorf("create %s certificate: %w", name, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode %s key: %w", name, err)
	}
	if err := writeFile(filepath.Join(dir, name+".key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, name+".crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func randomHex(byteCount int) ([]byte, error) {
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	encoded := make([]byte, hex.EncodedLen(len(raw)))
	hex.Encode(encoded, raw)
	return encoded, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		return big.NewInt(1), nil
	}
	return serial, nil
}

func writeFile(path string, contents []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, contents, mode); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func readCertificate(path string) (*x509.Certificate, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read certificate %s: %w", path, err)
	}
	block, _ := pem.Decode(contents)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("invalid PEM certificate: %s", path)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate %s: %w", path, err)
	}
	return certificate, nil
}
