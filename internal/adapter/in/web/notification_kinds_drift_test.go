package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The web client refuses a notification kind it does not know (BAD_RESPONSE),
// so a kind the server posts and the client lacks takes the whole inbox down
// (ADR 0079). The two lists must hold the same kinds, in both directions.
func TestNotificationKindsOfTheServerAndTheWebClientAreTheSame(t *testing.T) {
	server := serverNotificationKinds(t)
	client := webNotificationKinds(t)
	if missing := difference(server, client); len(missing) > 0 {
		t.Errorf("the server posts kinds the web client does not know (add them to KINDS in web/src/modules/notifications/domain/notification.js and to the notifications contract): %v", missing)
	}
	if extra := difference(client, server); len(extra) > 0 {
		t.Errorf("the web client knows kinds the server never posts (remove them from KINDS or add them to domain.NotificationKind): %v", extra)
	}
}

// serverNotificationKinds reads the NotificationKind constants of the domain.
func serverNotificationKinds(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../../../domain/notification.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "NotificationKind" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok {
					t.Fatalf("NotificationKind constant %v is not a string literal", vs.Names)
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				kinds = append(kinds, s)
			}
		}
	}
	if len(kinds) == 0 {
		t.Fatal("found no NotificationKind constants in internal/domain/notification.go")
	}
	return kinds
}

var kindsList = regexp.MustCompile(`(?s)export const KINDS = Object\.freeze\(\[(.*?)\]\)`)

// webNotificationKinds reads KINDS from the web client's notification domain.
func webNotificationKinds(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../../../../web/src/modules/notifications/domain/notification.js")
	if err != nil {
		t.Fatal(err)
	}
	m := kindsList.FindSubmatch(src)
	if m == nil {
		t.Fatal("found no `export const KINDS = Object.freeze([...])` in the web notification domain")
	}
	var kinds []string
	for _, part := range strings.Split(string(m[1]), ",") {
		if s := strings.Trim(strings.TrimSpace(part), `"`); s != "" {
			kinds = append(kinds, s)
		}
	}
	return kinds
}

// difference returns what a holds and b does not.
func difference(a, b []string) []string {
	var out []string
	for _, k := range a {
		if !slices.Contains(b, k) {
			out = append(out, k)
		}
	}
	return out
}
