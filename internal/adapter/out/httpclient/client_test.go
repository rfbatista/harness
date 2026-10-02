package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"
)

func TestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/coded":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"ticket not found","code":"TICKET_NOT_FOUND"}`))
		case "/uncoded":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid body"}`))
		default:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`<html>proxy</html>`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL)
	ctx := context.Background()

	err := c.get(ctx, "/coded", nil, nil)
	if errs.Code(err) != "TICKET_NOT_FOUND" || err.Error() == "" {
		t.Fatalf("coded response = %v, want TICKET_NOT_FOUND", err)
	}

	var se *StatusError
	if err := c.get(ctx, "/uncoded", nil, nil); !errors.As(err, &se) || se.Status != 400 || se.Message != "invalid body" {
		t.Fatalf("uncoded response = %#v, want StatusError 400 invalid body", err)
	}
	if err := c.get(ctx, "/other", nil, nil); !errors.As(err, &se) || se.Status != 502 {
		t.Fatalf("non-JSON response = %#v, want StatusError 502", err)
	}
}

func TestToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	if err := New(srv.URL, WithToken("s3cret")).Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer s3cret" {
		t.Fatalf("Authorization = %q, want the bearer token", got)
	}
	if err := New(srv.URL).Health(context.Background()); err != nil || got != "" {
		t.Fatalf("no token: Authorization = %q, err %v", got, err)
	}
}

// A response well past any fixed read cap still decodes: agents come back
// with their skills' files and have been seen at 19MB.
func TestLargeResponse(t *testing.T) {
	big := strings.Repeat("x", 17<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"` + big + `"}`))
	}))
	defer srv.Close()

	var out struct {
		Name string `json:"name"`
	}
	if err := New(srv.URL).get(context.Background(), "/big", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Name) != len(big) {
		t.Fatalf("decoded %d bytes, want %d", len(out.Name), len(big))
	}
}
