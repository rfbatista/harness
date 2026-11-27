package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"operators-mcp/internal/adapter/out/personcred"
)

// pair redeems the pairing code the harness printed for the credential and keeps
// it in the harness's own 0600 file, so this client and the
// bench command send it from then on (ADR 0016). The one-time code is the only
// secret that passes through an argument, and it is spent by this call.
func pair(args []string) int {
	fs := flag.NewFlagSet("tui-client pair", flag.ContinueOnError)
	api := fs.String("api", envOr("CODING_POOL_API", "http://localhost:8080"), "coding_pool server URL")
	code := fs.String("code", "", "the pairing code from the link the server printed")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *code == "" {
		fmt.Fprintln(os.Stderr, "tui-client pair: --code is required: it is the code=… part of the link the server prints when nobody is paired")
		return 2
	}
	credential, err := redeem(*api, *code)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui-client pair:", err)
		return 1
	}
	if err := personcred.Save(credential); err != nil {
		fmt.Fprintln(os.Stderr, "tui-client pair: the credential could not be stored:", err)
		return 1
	}
	fmt.Println("Paired: this client and the bench command now send the person credential.")
	return 0
}

// redeem posts the code to the server and answers the credential it gives.
func redeem(api, code string) (string, error) {
	body, _ := json.Marshal(map[string]string{"code": code})
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Post(strings.TrimRight(api, "/")+"/api/person/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		Credential string `json:"credential"`
		Error      string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != http.StatusOK || out.Credential == "" {
		msg := out.Error
		if msg == "" {
			msg = res.Status
		}
		return "", fmt.Errorf("the server refused the code: %s", msg)
	}
	return out.Credential, nil
}
