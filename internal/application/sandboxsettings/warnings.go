package sandboxsettings

import (
	"os"
	"path/filepath"
	"strings"
)

// The warnings a path can carry. They never stop it being saved.
const (
	// WarnMissing: nothing exists at the path yet.
	WarnMissing = "missing"
	// WarnCredentials: the path is, or is inside, a folder that usually holds
	// credentials.
	WarnCredentials = "credentials"
)

// credentialDirs are the folders, relative to the home directory, that
// usually hold secrets: keys, cloud and cluster credentials, keychains and
// the CLIs' stored logins.
var credentialDirs = []string{
	".ssh", ".aws", ".gnupg", ".kube", ".docker", ".azure", ".netrc",
	".config/gcloud", ".config/gh", ".config/hub", ".config/doctl",
	".config/op", ".password-store", ".terraform.d",
	"Library/Keychains",
}

// PathWarnings says what is worth a second look about a path: that it does
// not exist yet, and that it is, or is inside, a credential folder. The path
// may start with ~.
func (s *Store) PathWarnings(path string) []string {
	abs := s.expand(path)
	out := []string{}
	if _, err := os.Stat(abs); err != nil && os.IsNotExist(err) {
		out = append(out, WarnMissing)
	}
	if s.Home != "" && s.inCredentialDir(abs) {
		out = append(out, WarnCredentials)
	}
	return out
}

func (s *Store) inCredentialDir(abs string) bool {
	for _, d := range credentialDirs {
		dir := filepath.Join(filepath.Clean(s.Home), filepath.FromSlash(d))
		if abs == dir || strings.HasPrefix(abs, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
