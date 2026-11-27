package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/personcred"
)

func TestPairRedeemsTheCodeAndStoresTheCredentialWithoutPuttingItInAnArgumentOrTheEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/person/pair" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		buf := new(strings.Builder)
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		buf.Write(b[:n])
		if !strings.Contains(buf.String(), `"code":"GOODCODE"`) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"that pairing code is wrong","code":"PAIRING_CODE_INVALID"}`))
			return
		}
		_, _ = w.Write([]byte(`{"credential":"the-credential"}`))
	}))
	defer srv.Close()

	old := personcred.Default
	t.Cleanup(func() { personcred.Default = old })
	file := filepath.Join(t.TempDir(), "person.cred")
	personcred.Default = personcred.FileStore{PathFn: func() (string, error) { return file, nil }}

	if code := pair([]string{"--api", srv.URL, "--code", "WRONG"}); code != 1 {
		t.Errorf("a wrong code exited %d, want 1", code)
	}
	if _, err := personcred.Load(); err == nil {
		t.Error("a wrong code stored a credential")
	}
	if code := pair([]string{"--api", srv.URL, "--code", "GOODCODE"}); code != 0 {
		t.Fatalf("pairing exited %d", code)
	}
	if got, err := personcred.Load(); err != nil || got != "the-credential" {
		t.Errorf("stored %q, %v", got, err)
	}
	if code := pair(nil); code != 2 {
		t.Errorf("no code exited %d, want 2", code)
	}
}
