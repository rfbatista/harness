package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/personcred"
)

func pairingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/person/pair" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		if !strings.Contains(string(b[:n]), `"code":"GOODCODE"`) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"that pairing code is wrong","code":"PAIRING_CODE_INVALID"}`))
			return
		}
		_, _ = w.Write([]byte(`{"credential":"the-credential"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withFileStore(t *testing.T) {
	t.Helper()
	old := personcred.Default
	t.Cleanup(func() { personcred.Default = old })
	file := filepath.Join(t.TempDir(), "person.cred")
	personcred.Default = personcred.FileStore{PathFn: func() (string, error) { return file, nil }}
}

func TestPairReadsTheCodeFromStdinNotFromAnArgumentAndStoresTheCredential(t *testing.T) {
	srv := pairingServer(t)
	withFileStore(t)
	var out, errOut bytes.Buffer

	// A wrong code stores nothing.
	if code := pairFrom([]string{"--api", srv.URL}, strings.NewReader("WRONG\n"), false, &out, &errOut); code != 1 {
		t.Errorf("a wrong code exited %d, want 1", code)
	}
	if _, err := personcred.Load(); err == nil {
		t.Error("a wrong code stored a credential")
	}
	// The code arrives on stdin, with or without a trailing newline.
	for _, in := range []string{"GOODCODE\n", "GOODCODE", "  GOODCODE  \r\n"} {
		withFileStore(t)
		if code := pairFrom([]string{"--api", srv.URL}, strings.NewReader(in), false, &out, &errOut); code != 0 {
			t.Fatalf("pairing with stdin %q exited %d: %s", in, code, errOut.String())
		}
		if got, err := personcred.Load(); err != nil || got != "the-credential" {
			t.Errorf("stored %q, %v", got, err)
		}
	}
	// Nothing to read is a usage error, and the credential never reaches the output.
	if code := pairFrom([]string{"--api", srv.URL}, strings.NewReader(""), false, &out, &errOut); code != 2 {
		t.Errorf("an empty stdin exited %d, want 2", code)
	}
	if strings.Contains(out.String()+errOut.String(), "the-credential") {
		t.Error("the credential was printed")
	}
}

func TestPairNoLongerTakesTheCodeAsAnArgument(t *testing.T) {
	srv := pairingServer(t)
	withFileStore(t)
	var out, errOut bytes.Buffer
	// An argument carries the code to every process of the user (KERN_PROCARGS2):
	// the flag is gone, so the command refuses it rather than using it.
	if code := pairFrom([]string{"--api", srv.URL, "--code", "GOODCODE"}, strings.NewReader("GOODCODE\n"), false, &out, &errOut); code != 2 {
		t.Errorf("--code exited %d, want 2 (an unknown flag)", code)
	}
	if _, err := personcred.Load(); err == nil {
		t.Error("a code given as an argument was redeemed")
	}
}

func TestAPromptOnATerminalHidesTheTypedCode(t *testing.T) {
	srv := pairingServer(t)
	withFileStore(t)
	var out, errOut bytes.Buffer
	var calls []string
	restore := echoControl
	echoControl = func(on bool) error {
		if on {
			calls = append(calls, "on")
		} else {
			calls = append(calls, "off")
		}
		return nil
	}
	t.Cleanup(func() { echoControl = restore })

	if code := pairFrom([]string{"--api", srv.URL}, strings.NewReader("GOODCODE\n"), true, &out, &errOut); code != 0 {
		t.Fatalf("exited %d: %s", code, errOut.String())
	}
	if len(calls) != 2 || calls[0] != "off" || calls[1] != "on" {
		t.Errorf("echo was switched %v, want off then on again", calls)
	}
	if !strings.Contains(out.String()+errOut.String(), "not shown") {
		t.Errorf("the prompt does not say the code is not shown: %q", out.String()+errOut.String())
	}
}
