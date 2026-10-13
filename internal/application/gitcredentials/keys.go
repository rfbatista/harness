package gitcredentials

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"

	"operators-mcp/internal/domain"
)

// sshKey is a private key as the harness keeps it: unencrypted OpenSSH PEM
// (sealed at rest), with what is shown of it.
type sshKey struct {
	pem         []byte
	publicKey   string // the authorized_keys line
	fingerprint string
}

func invalidKey(msg string) error {
	return domain.GitCredentialError(domain.CodeGitCredentialInvalidKey, msg)
}

// parseSSHKey reads an OpenSSH or PEM private key, decrypting it with
// passphrase when it is encrypted, and re-encodes it unencrypted: the
// passphrase is used here and never stored.
func parseSSHKey(text, passphrase string) (*sshKey, error) {
	raw := []byte(strings.TrimSpace(text) + "\n")
	key, err := ssh.ParseRawPrivateKey(raw)
	var missing *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &missing):
		if passphrase == "" {
			return nil, invalidKey("the private key is encrypted; give its passphrase")
		}
		key, err = ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte(passphrase))
		if err != nil {
			return nil, invalidKey("the passphrase does not decrypt the private key")
		}
	case err != nil:
		return nil, invalidKey("the private key does not parse as an OpenSSH or PEM key")
	}
	return encodeSSHKey(key)
}

// generateSSHKey makes a new ed25519 key.
func generateSSHKey() (*sshKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return encodeSSHKey(priv)
}

func encodeSSHKey(key any) (*sshKey, error) {
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, invalidKey("unsupported private key type")
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		return nil, invalidKey("unsupported private key type")
	}
	pub := signer.PublicKey()
	return &sshKey{
		pem:         pem.EncodeToMemory(block),
		publicKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
		fingerprint: ssh.FingerprintSHA256(pub),
	}, nil
}

// tokenFingerprint shows a token by its last four characters.
func tokenFingerprint(token string) string {
	r := []rune(token)
	if len(r) > 4 {
		r = r[len(r)-4:]
	}
	return "…" + string(r)
}
