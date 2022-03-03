package security

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/openshift/cluster-logging-operator/internal/constants"
	corev1 "k8s.io/api/core/v1"
)

type TLS bool

type HostnameVerify bool

type TLSCertKey struct {
	CertPath string
	KeyPath  string
}

type UserNamePass struct {
	UsernamePath string
	PasswordPath string
}

type SharedKey struct {
	Key string
}

type CAFile struct {
	CAFilePath string
}

type Passphrase struct {
	PassphrasePath string
}

var NoSecrets = map[string]*corev1.Secret{}

func HasUsernamePassword(secret *corev1.Secret) bool {
	return HasKeys(secret, constants.ClientUsername, constants.ClientPassword)
}

func HasTLSCertAndKey(secret *corev1.Secret) bool {
	return HasKeys(secret, constants.ClientCertKey, constants.ClientPrivateKey)
}

func HasCABundle(secret *corev1.Secret) bool {
	return HasKeys(secret, constants.TrustedCABundleKey)
}

func HasSharedKey(secret *corev1.Secret) bool {
	return HasKeys(secret, constants.SharedKey)
}

func HasPassphrase(secret *corev1.Secret) bool {
	return HasKeys(secret, constants.Passphrase)
}

func HasAwsCredentialsKey(secret *corev1.Secret) bool {
	return HasKeys(secret, constants.AWSCredentialsKey)
}

// GetKey if found return value and ok=true, else ok=false
func GetKey(secret *corev1.Secret, key string) (data []byte, ok bool) {
	if secret == nil {
		return nil, false
	}
	data, ok = secret.Data[key]
	return data, ok
}

// HasKeys true if all keys are present.
func HasKeys(secret *corev1.Secret, keys ...string) bool {
	for _, k := range keys {
		_, ok := GetKey(secret, k)
		if !ok {
			return false
		}
	}
	return true
}

func SecretPath(name string, file string) string {
	return fmt.Sprintf("'%s'", filepath.Join("/var/run/ocp-collector/secrets", name, file))
}

// TryKeys try keys in turn return data for fist one present with ok=true.
// If none present return ok=false.
func TryKeys(secret *corev1.Secret, keys ...string) (data []byte, ok bool) {
	for _, k := range keys {
		data, ok := GetKey(secret, k)
		if ok {
			return data, true
		}
	}
	return nil, false
}

func GetFromSecret(secret *corev1.Secret, name string) string {
	if secret != nil {
		return string(secret.Data[name])
	}
	return ""
}

// ParseRoleArn search for 'arn:aws:iam:: in the credentials string and return string up to end of line
func ParseRoleArn(secret *corev1.Secret) string {
	credentials := GetFromSecret(secret, constants.AWSCredentialsKey)
	if credentials != "" {
		roleIndex := strings.Index(credentials, "arn:aws:iam::")
		if roleIndex != -1 { // found the role arn string
			roleArn := strings.Split(credentials[roleIndex:], "\n")
			return roleArn[0]
		}
	}
	return ""
}

// ParseIdentityToken split credentials string at 'web_identity_token_file = ' and return everything after
// Return volume mount path and token file path, or default values if exact separator key is not found
func ParseIdentityToken(secret *corev1.Secret) (mountPath, filePath string) {
	credentials := GetFromSecret(secret, constants.AWSCredentialsKey)
	if credentials != "" {
		split := strings.Split(credentials, "web_identity_token_file = ")
		if split[0] != credentials { // found the separator
			tokenMountPathWithFile := split[1]
			return filepath.Dir(tokenMountPathWithFile), filepath.Base(tokenMountPathWithFile)
		}
	}
	// Use default
	return constants.AWSWebIdentityTokenMount, constants.AWSWebIdentityTokenFilePath
}
