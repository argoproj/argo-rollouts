// Utility functions for managing HTTPS server certificates
// entries for ArgoCD
package cert

import (
	"bufio"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"

	"github.com/argoproj/argo-rollouts/common"
)

// A representation of a TLS certificate
type TLSCertificate struct {
	// Subject of the certificate
	Subject string
	// Issuer of the certificate
	Issuer string
	// Certificate data
	Data string
}

// Helper struct for certificate selection
type CertificateListSelector struct {
	// Pattern to match the hostname with
	HostNamePattern string
	// Type of certificate to match
	CertType string
	// Subtype of certificate to match
	CertSubType string
}

const (
	// Text marker indicating start of certificate in PEM format
	CertificateBeginMarker = "-----BEGIN CERTIFICATE-----"
	// Text marker indicating end of certificate in PEM format
	CertificateEndMarker = "-----END CERTIFICATE-----"
	// Maximum number of lines for a single certificate
	CertificateMaxLines = 128
	// Maximum number of certificates or known host entries in a stream
	CertificateMaxEntriesPerStream = 256
)

// Regular expression that matches a valid hostname
var validHostNameRegexp = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9_][a-zA-Z0-9-_]{0,61}[a-zA-Z0-9_])(\.([a-zA-Z0-9]|[a-zA-Z0-9_][a-zA-Z0-9-_]{0,61}[a-zA-Z0-9]))*(\.){0,1}$`)

// Regular expression that matches all kind of IPv6 addresses
// See https://stackoverflow.com/questions/53497/regular-expression-that-matches-valid-ipv6-addresses
var validIPv6Regexp = regexp.MustCompile(`(([0-9a-fA-F]{1,4}:){7,7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:)|fe80:(:[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(ffff(:0{1,4}){0,1}:){0,1}((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])|([0-9a-fA-F]{1,4}:){1,4}:((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9]))`)

// Regular expression that matches a valid FQDN
var validFQDNRegexp = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9])(\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9]))*(\.){1}$`)

// Can be used to test whether a given string represents a valid hostname
// If fqdn is true, given string must also be a FQDN representation.
func IsValidHostname(hostname string, fqdn bool) bool {
	if !fqdn {
		return validHostNameRegexp.Match([]byte(hostname)) || validIPv6Regexp.Match([]byte(hostname))
	} else {
		return validFQDNRegexp.Match([]byte(hostname))
	}
}

// Get the configured path to where TLS certificates are stored on the local
// filesystem. If ARGOCD_TLS_DATA_PATH environment is set, path is taken from
// there, otherwise the default will be returned.
func GetTLSCertificateDataPath() string {
	if envPath := os.Getenv(common.EnvVarTLSDataPath); envPath != "" {
		return envPath
	} else {
		return common.DefaultPathTLSConfig
	}
}

// Decode a certificate in PEM format to X509 data structure
func DecodePEMCertificateToX509(pemData string) (*x509.Certificate, error) {
	decodedData, _ := pem.Decode([]byte(pemData))
	if decodedData == nil {
		return nil, errors.New("could not decode PEM data from input")
	}
	x509Cert, err := x509.ParseCertificate(decodedData.Bytes)
	if err != nil {
		return nil, errors.New("could not parse X509 data from input")
	}
	return x509Cert, nil
}

// Parse TLS certificates from a multiline string
func ParseTLSCertificatesFromData(data string) ([]string, error) {
	return ParseTLSCertificatesFromStream(strings.NewReader(data))
}

// Parse TLS certificates from a file
func ParseTLSCertificatesFromPath(sourceFile string) ([]string, error) {
	fileHandle, err := os.Open(sourceFile)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err = fileHandle.Close(); err != nil {
			log.WithFields(log.Fields{
				common.SecurityField:    common.SecurityMedium,
				common.SecurityCWEField: common.SecurityCWEMissingReleaseOfFileDescriptor,
			}).Errorf("error closing file %q: %v", fileHandle.Name(), err)
		}
	}()
	return ParseTLSCertificatesFromStream(fileHandle)
}

// Parse TLS certificate data from a data stream. The stream may contain more
// than one certificate. Each found certificate will generate a unique entry
// in the returned slice, so the length of the slice indicates how many
// certificates have been found.
func ParseTLSCertificatesFromStream(stream io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(stream)
	inCertData := false
	pemData := ""
	curLine := 0
	certLine := 0

	certificateList := make([]string, 0)

	// TODO: Implement maximum amount of data to parse
	// TODO: Implement error heuristics

	for scanner.Scan() {
		curLine += 1
		if !inCertData {
			if strings.HasPrefix(scanner.Text(), CertificateBeginMarker) {
				certLine = 1
				inCertData = true
				pemData += scanner.Text() + "\n"
			}
		} else {
			certLine += 1
			pemData += scanner.Text() + "\n"
			if strings.HasPrefix(scanner.Text(), CertificateEndMarker) {
				inCertData = false
				certificateList = append(certificateList, pemData)
				pemData = ""
			}
		}

		if certLine > CertificateMaxLines {
			return nil, errors.New("maximum number of lines exceeded during certificate parsing")
		}
	}

	return certificateList, nil
}

// Parse a raw known hosts line into a PublicKey object and a list of hosts the
// key would be valid for.
func KnownHostsLineToPublicKey(line string) ([]string, ssh.PublicKey, error) {
	_, hostnames, keyData, _, _, err := ssh.ParseKnownHosts([]byte(line))
	if err != nil {
		return nil, nil, err
	}
	return hostnames, keyData, nil
}

func TokenizedDataToPublicKey(hostname string, subType string, rawKeyData string) ([]string, ssh.PublicKey, error) {
	hostnames, keyData, err := KnownHostsLineToPublicKey(fmt.Sprintf("%s %s %s", hostname, subType, rawKeyData))
	if err != nil {
		return nil, nil, err
	}
	return hostnames, keyData, nil
}

// Returns the requested pattern with all possible square brackets escaped
func nonBracketedPattern(pattern string) string {
	ret := strings.ReplaceAll(pattern, "[", `\[`)
	return strings.ReplaceAll(ret, "]", `\]`)
}

// We do not use full fledged regular expression for matching the hostname.
// Instead, we use a less expensive file system glob, which should be fully
// sufficient for our use case.
func MatchHostName(hostname, pattern string) bool {
	// If pattern is empty, we always return a match
	if pattern == "" {
		return true
	}
	match, err := filepath.Match(nonBracketedPattern(pattern), hostname)
	if err != nil {
		return false
	}
	return match
}

// Remove possible port number from hostname and return just the FQDN
func ServerNameWithoutPort(serverName string) string {
	return strings.Split(serverName, ":")[0]
}

// Load certificate data from a file. If the file does not exist, we do not
// consider it an error and just return empty data.
func GetCertificateForConnect(serverName string) ([]string, error) {
	dataPath := GetTLSCertificateDataPath()
	certPath, err := filepath.Abs(filepath.Join(dataPath, ServerNameWithoutPort(serverName)))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(certPath, dataPath) {
		return nil, fmt.Errorf("could not get certificate for host %s", serverName)
	}
	certificates, err := ParseTLSCertificatesFromPath(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	if len(certificates) == 0 {
		return nil, fmt.Errorf("no certificates found in existing file")
	}

	return certificates, nil
}

// Gets the full path for a certificate bundle configured from a ConfigMap
// mount. This function makes sure that the path returned actually contain
// at least one valid certificate, and no invalid data.
func GetCertBundlePathForRepository(serverName string) (string, error) {
	certPath := filepath.Join(GetTLSCertificateDataPath(), ServerNameWithoutPort(serverName))
	certs, err := GetCertificateForConnect(serverName)
	if err != nil {
		return "", nil
	}
	if len(certs) == 0 {
		return "", nil
	}
	return certPath, nil
}

// Convert a list of certificates in PEM format to a x509.CertPool object,
// usable for most golang TLS functions.
func GetCertPoolFromPEMData(pemData []string) *x509.CertPool {
	certPool := x509.NewCertPool()
	for _, pem := range pemData {
		certPool.AppendCertsFromPEM([]byte(pem))
	}
	return certPool
}
