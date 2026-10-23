//go:build !windows

package shim

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"operators-mcp/internal/ports"
)

// realSecurity is the macOS keychain command every call the shim does not
// answer goes to.
const realSecurity = "/usr/bin/security"

// claudeKeychainService is what the service name of Claude's login item
// starts with (claude adds a hash of its config dir for a custom one).
const claudeKeychainService = "Claude Code-credentials"

// Exit codes the shim answers: security's own "item not found", and a plain
// failure for a write, which makes claude keep its login in .credentials.json.
const (
	exitItemNotFound = 44
	exitWriteRefused = 1
)

// securityMain is the `security` shim: a find, add or delete of a generic
// password whose service starts with "Claude Code-credentials" answers as
// if the keychain held nothing and could not be written, and touches no
// keychain. Every other call, in argument form or in `-i` stdin form, runs
// real with the same args, stdin, stdout, stderr and exit code.
func securityMain(args []string, stdin io.Reader, stdout, stderr io.Writer, real string) int {
	interactive := slices.Contains(args, "-i")
	if !interactive {
		if code, ok := claudeKeychainCall(args); ok {
			return code
		}
		return runReal(real, args, stdin, stdout, stderr)
	}
	// `security -i` reads its commands from stdin; claude writes its item
	// that way. The first line decides; the real one gets every line back.
	br := bufio.NewReader(stdin)
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return runReal(real, args, br, stdout, stderr)
	}
	if code, ok := claudeKeychainCall(tokens(line)); ok {
		return code
	}
	return runReal(real, args, io.MultiReader(strings.NewReader(line), br), stdout, stderr)
}

// claudeKeychainCall says whether args are a find, add or delete of a generic
// password of Claude's, and the exit code answering it.
func claudeKeychainCall(args []string) (int, bool) {
	var cmd string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-p" { // the prompt option's value is not the command
			i++
			continue
		}
		if !strings.HasPrefix(a, "-") {
			cmd, args = a, args[i+1:]
			break
		}
	}
	var code int
	switch cmd {
	case "find-generic-password", "delete-generic-password":
		code = exitItemNotFound
	case "add-generic-password":
		code = exitWriteRefused
	default:
		return 0, false
	}
	if !strings.HasPrefix(serviceOf(args), claudeKeychainService) {
		return 0, false
	}
	return code, true
}

// serviceOf is the -s option's value ("-s name" or "-sname"); "" when none.
func serviceOf(args []string) string {
	for i, a := range args {
		switch {
		case a == "-s" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, "-s") && len(a) > 2 && !strings.HasPrefix(a, "--"):
			return a[2:]
		}
	}
	return ""
}

// tokens splits a command line the way security's interactive mode does:
// on spaces, with single or double quotes and backslash escapes.
func tokens(line string) []string {
	var out []string
	var cur strings.Builder
	in, quote := false, rune(0)
	esc := false
	for _, r := range strings.TrimRight(line, "\r\n") {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc, in = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, in = r, true
		case r == ' ' || r == '\t':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

// runReal runs the real command and answers its exit code. A stdin that is
// not a file reaches it through a pipe that goroutine fills: the command's
// exit must not wait for a caller that keeps its own pipe open.
func runReal(real string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := exec.Command(real, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Env = os.Environ()
	if f, ok := stdin.(*os.File); ok {
		cmd.Stdin = f
	} else if r, w, err := os.Pipe(); err == nil {
		cmd.Stdin = r
		defer r.Close()
		go func() {
			_, _ = io.Copy(w, stdin)
			_ = w.Close()
		}()
	}
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	fmt.Fprintf(stderr, "%s: %v\n", ports.SecurityShimCommand, err)
	return 127
}
