package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"operators-mcp/internal/adapter/out/personcred"
)

// pair redeems the pairing code the harness printed for the credential and keeps
// it in the harness's own 0600 file, so this client and the bench command send it
// from then on (ADR 0016). The code is read from stdin, with the typed characters
// hidden on a terminal, and never from an argument: any process of the user, a
// sandboxed session included, can read another process's arguments and redeem the
// code first.
func pair(args []string) int {
	return pairFrom(args, os.Stdin, isTerminal(os.Stdin), os.Stdout, os.Stderr)
}

func pairFrom(args []string, stdin io.Reader, terminal bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tui-client pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	api := fs.String("api", envOr("CODING_POOL_API", "http://localhost:8080"), "coding_pool server URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	code, err := readCode(stdin, terminal, stderr)
	if err != nil || code == "" {
		fmt.Fprintln(stderr, "tui-client pair: give the pairing code on stdin: it is the code=… part of the link the server prints when nobody is paired")
		return 2
	}
	credential, err := redeem(*api, code)
	if err != nil {
		fmt.Fprintln(stderr, "tui-client pair:", err)
		return 1
	}
	if err := personcred.Save(credential); err != nil {
		fmt.Fprintln(stderr, "tui-client pair: the credential could not be stored:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Paired: this client and the bench command now send the person credential.")
	return 0
}

// echoControl switches the terminal's echo; tests replace it.
var echoControl = func(on bool) error {
	arg := "-echo"
	if on {
		arg = "echo"
	}
	c := exec.Command("stty", arg)
	c.Stdin = os.Stdin
	return c.Run()
}

// isTerminal says whether f is a terminal (a character device).
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// readCode reads one line from stdin. On a terminal it prompts and hides what is
// typed.
func readCode(stdin io.Reader, terminal bool, prompt io.Writer) (string, error) {
	if terminal {
		fmt.Fprint(prompt, "Pairing code (not shown): ")
		if err := echoControl(false); err == nil {
			defer func() {
				_ = echoControl(true)
				fmt.Fprintln(prompt)
			}()
		}
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
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
