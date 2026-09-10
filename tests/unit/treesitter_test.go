package unit

import (
	"strings"
	"testing"

	"operators-mcp/internal/application/tooling"
)

// ---------------------------------------------------------------------------
// TSFindErrors — syntax validation via tree-sitter AST
// ---------------------------------------------------------------------------

func TestTSFindErrors_ValidGo(t *testing.T) {
	src := []byte("package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n")
	errors := tooling.TSFindErrors(src, "main.go")
	if len(errors) > 0 {
		t.Errorf("expected no errors for valid Go, got %d: %v", len(errors), errors)
	}
}

func TestTSFindErrors_InvalidGo(t *testing.T) {
	src := []byte("package main\n\nfunc main() {\n\tx :=\n}\n")
	errors := tooling.TSFindErrors(src, "main.go")
	if len(errors) == 0 {
		t.Error("expected syntax errors for invalid Go")
	}
}

func TestTSFindErrors_ValidJS(t *testing.T) {
	src := []byte("function hello() {\n  return 42;\n}\n")
	errors := tooling.TSFindErrors(src, "app.js")
	if len(errors) > 0 {
		t.Errorf("expected no errors for valid JS, got %d", len(errors))
	}
}

func TestTSFindErrors_InvalidJS(t *testing.T) {
	src := []byte("function hello( {\n  return 42;\n}\n")
	errors := tooling.TSFindErrors(src, "app.js")
	if len(errors) == 0 {
		t.Error("expected syntax errors for invalid JS (missing closing paren)")
	}
}

func TestTSFindErrors_ValidTS(t *testing.T) {
	src := []byte("interface User {\n  name: string;\n  age: number;\n}\n\nfunction greet(u: User): string {\n  return u.name;\n}\n")
	errors := tooling.TSFindErrors(src, "app.ts")
	if len(errors) > 0 {
		t.Errorf("expected no errors for valid TS, got %d", len(errors))
	}
}

func TestTSFindErrors_InvalidTS(t *testing.T) {
	src := []byte("function greet(name: string) string {\n  return name;\n}\n")
	errors := tooling.TSFindErrors(src, "app.ts")
	if len(errors) == 0 {
		t.Error("expected syntax errors for invalid TS (missing colon before return type)")
	}
}

func TestTSFindErrors_ValidPython(t *testing.T) {
	src := []byte("def hello():\n    return 42\n")
	errors := tooling.TSFindErrors(src, "app.py")
	if len(errors) > 0 {
		t.Errorf("expected no errors for valid Python, got %d", len(errors))
	}
}

func TestTSFindErrors_ValidTSX(t *testing.T) {
	src := []byte("function App() {\n  return <div>Hello</div>;\n}\n")
	errors := tooling.TSFindErrors(src, "App.tsx")
	if len(errors) > 0 {
		t.Errorf("expected no errors for valid TSX, got %d", len(errors))
	}
}

func TestTSFindErrors_UnsupportedExtension(t *testing.T) {
	src := []byte("some random content")
	errors := tooling.TSFindErrors(src, "readme.md")
	if errors != nil {
		t.Errorf("expected nil for unsupported extension, got %v", errors)
	}
}

func TestFormatSyntaxErrors_Empty(t *testing.T) {
	result := tooling.FormatSyntaxErrors(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestFormatSyntaxErrors_WithErrors(t *testing.T) {
	errors := []tooling.SyntaxError{
		{Line: 3, Column: 5, EndLine: 3, Kind: "ERROR", Context: "x :="},
	}
	result := tooling.FormatSyntaxErrors(errors)
	if !strings.Contains(result, "ERROR") || !strings.Contains(result, "line 3") {
		t.Errorf("expected formatted error, got %q", result)
	}
}

func TestTSLint_IntegratesWithLinter(t *testing.T) {
	result := tooling.TSLint([]byte("function foo( {\n  return;\n}\n"), "broken.js")
	if result == "" {
		t.Error("expected tree-sitter lint warning for broken JS")
	}
}

// ---------------------------------------------------------------------------
// TSExtractSymbols — symbol extraction for repo map
// ---------------------------------------------------------------------------

func TestTSExtractSymbols_Go(t *testing.T) {
	src := []byte(`package main

type Service struct {
	name string
}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Run() error {
	return nil
}

type Handler interface {
	Handle()
}
`)
	symbols := tooling.TSExtractSymbols(src, "service.go")
	if len(symbols) == 0 {
		t.Fatal("expected symbols from Go file")
	}

	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	if names["Service"] == "" {
		t.Error("expected Service type/struct")
	}
	if names["NewService"] != "function" {
		t.Errorf("expected NewService as function, got %q", names["NewService"])
	}
	if names["Handler"] == "" {
		t.Error("expected Handler interface")
	}
}

func TestTSExtractSymbols_JS(t *testing.T) {
	src := []byte(`function handleRequest(req, res) {
  return res.send("ok");
}

class Router {
  constructor() {}
  addRoute(path) {}
}

const helper = () => {};
`)
	symbols := tooling.TSExtractSymbols(src, "app.js")
	if len(symbols) == 0 {
		t.Fatal("expected symbols from JS file")
	}

	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	if names["handleRequest"] != "function" {
		t.Errorf("expected handleRequest as function, got %q", names["handleRequest"])
	}
	if names["Router"] != "class" {
		t.Errorf("expected Router as class, got %q", names["Router"])
	}
}

func TestTSExtractSymbols_TS(t *testing.T) {
	src := []byte(`interface UserService {
  getUser(id: string): User;
}

type UserId = string;

export function createUser(name: string): User {
  return { name };
}

enum Role {
  Admin,
  User,
}
`)
	symbols := tooling.TSExtractSymbols(src, "user.ts")
	if len(symbols) == 0 {
		t.Fatal("expected symbols from TS file")
	}

	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	if names["UserService"] != "interface" {
		t.Errorf("expected UserService as interface, got %q", names["UserService"])
	}
	if names["UserId"] != "type" {
		t.Errorf("expected UserId as type, got %q", names["UserId"])
	}
	if names["createUser"] != "function" {
		t.Errorf("expected createUser as function, got %q", names["createUser"])
	}
	if names["Role"] != "enum" {
		t.Errorf("expected Role as enum, got %q", names["Role"])
	}
}

func TestTSExtractSymbols_Python(t *testing.T) {
	src := []byte(`class UserRepository:
    def __init__(self):
        pass

def get_user(user_id):
    return None

class AdminService:
    pass
`)
	symbols := tooling.TSExtractSymbols(src, "repo.py")
	if len(symbols) == 0 {
		t.Fatal("expected symbols from Python file")
	}

	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	if names["UserRepository"] != "class" {
		t.Errorf("expected UserRepository as class, got %q", names["UserRepository"])
	}
	if names["get_user"] != "function" {
		t.Errorf("expected get_user as function, got %q", names["get_user"])
	}
	if names["AdminService"] != "class" {
		t.Errorf("expected AdminService as class, got %q", names["AdminService"])
	}
}

func TestTSExtractSymbols_UnsupportedFile(t *testing.T) {
	symbols := tooling.TSExtractSymbols([]byte("# heading"), "readme.md")
	if symbols != nil {
		t.Errorf("expected nil for unsupported extension, got %v", symbols)
	}
}
