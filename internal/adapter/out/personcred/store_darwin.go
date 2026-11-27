//go:build darwin

package personcred

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

const (
	keychainService = "harness-person-credential"
	keychainAccount = "person"
	securityBin     = "/usr/bin/security"
)

func platformStore() Store { return keychainStore{fallback: FileStore{}} }

// keychainStore keeps the credential in the login keychain through `security`,
// and in the file when the keychain cannot be used.
type keychainStore struct{ fallback FileStore }

// Load implements Store. The secret comes back on stdout.
func (k keychainStore) Load() (string, error) {
	out, err := exec.Command(securityBin, "find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w").Output()
	if err == nil {
		if c := strings.TrimSpace(string(out)); c != "" {
			return c, nil
		}
	}
	return k.fallback.Load()
}

// Save implements Store. `security -i` reads its commands from stdin, so the
// credential is not in any process's arguments.
func (k keychainStore) Save(credential string) error {
	credential = strings.TrimSpace(credential)
	if strings.ContainsAny(credential, " \t\r\n'\"\\") {
		return errors.New("the credential has characters the keychain command cannot take")
	}
	cmd := exec.Command(securityBin, "-i")
	cmd.Stdin = bytes.NewBufferString("add-generic-password -U -s " + keychainService + " -a " + keychainAccount + " -w " + credential + "\n")
	if err := cmd.Run(); err != nil {
		return k.fallback.Save(credential)
	}
	return nil
}
