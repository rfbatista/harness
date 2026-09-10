package tooling

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// SyntaxError represents a single tree-sitter parse error found in the AST.
type SyntaxError struct {
	Line    int
	Column  int
	EndLine int
	Kind    string // "ERROR" or "MISSING"
	Context string
}

// Symbol represents a named definition extracted from a source file.
type Symbol struct {
	Name      string
	Kind      string // "function", "type", "class", "method", "interface", "variable"
	Line      int
	Signature string
}

var langExtMap = map[string]func() unsafe.Pointer{
	".go":  tree_sitter_go.Language,
	".js":  tree_sitter_javascript.Language,
	".jsx": tree_sitter_javascript.Language,
	".mjs": tree_sitter_javascript.Language,
	".cjs": tree_sitter_javascript.Language,
	".ts":  tree_sitter_typescript.LanguageTypescript,
	".mts": tree_sitter_typescript.LanguageTypescript,
	".cts": tree_sitter_typescript.LanguageTypescript,
	".tsx": tree_sitter_typescript.LanguageTSX,
	".py":  tree_sitter_python.Language,
}

func tsLanguageForExt(ext string) *tree_sitter.Language {
	fn, ok := langExtMap[ext]
	if !ok {
		return nil
	}
	return tree_sitter.NewLanguage(fn())
}

// TSSupportsExt returns true if tree-sitter can parse files with the given extension.
func TSSupportsExt(ext string) bool {
	_, ok := langExtMap[ext]
	return ok
}

// TSParse parses source code with the appropriate tree-sitter grammar.
// Returns nil if the extension is unsupported.
func TSParse(source []byte, path string) *tree_sitter.Tree {
	ext := filepath.Ext(path)
	lang := tsLanguageForExt(ext)
	if lang == nil {
		return nil
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(lang); err != nil {
		return nil
	}
	return parser.Parse(source, nil)
}

// TSFindErrors parses the source and walks the AST looking for ERROR and
// MISSING nodes, similar to Aider's linter.py approach.
func TSFindErrors(source []byte, path string) []SyntaxError {
	tree := TSParse(source, path)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	lines := strings.Split(string(source), "\n")
	var errors []SyntaxError

	cursor := tree.Walk()
	defer cursor.Close()

	walkErrors(cursor, lines, &errors)
	return errors
}

func walkErrors(cursor *tree_sitter.TreeCursor, lines []string, errors *[]SyntaxError) {
	node := cursor.Node()
	if node == nil {
		return
	}

	if node.IsError() || node.IsMissing() {
		kind := "ERROR"
		if node.IsMissing() {
			kind = "MISSING"
		}

		startRow := int(node.StartPosition().Row)
		ctx := ""
		if startRow < len(lines) {
			ctx = lines[startRow]
			if len(ctx) > 120 {
				ctx = ctx[:120] + "..."
			}
		}

		*errors = append(*errors, SyntaxError{
			Line:    startRow + 1,
			Column:  int(node.StartPosition().Column) + 1,
			EndLine: int(node.EndPosition().Row) + 1,
			Kind:    kind,
			Context: ctx,
		})
	}

	if node.HasError() && !node.IsError() {
		if cursor.GotoFirstChild() {
			walkErrors(cursor, lines, errors)
			for cursor.GotoNextSibling() {
				walkErrors(cursor, lines, errors)
			}
			cursor.GotoParent()
		}
	}
}

// FormatSyntaxErrors produces a human-readable summary of tree-sitter errors.
func FormatSyntaxErrors(errors []SyntaxError) string {
	if len(errors) == 0 {
		return ""
	}

	var b strings.Builder
	shown := errors
	if len(shown) > 5 {
		shown = shown[:5]
	}

	for _, e := range shown {
		if e.Context != "" {
			fmt.Fprintf(&b, "syntax %s at line %d col %d: %s\n", e.Kind, e.Line, e.Column, e.Context)
		} else {
			fmt.Fprintf(&b, "syntax %s at line %d col %d\n", e.Kind, e.Line, e.Column)
		}
	}

	if len(errors) > 5 {
		fmt.Fprintf(&b, "... and %d more syntax errors\n", len(errors)-5)
	}

	return strings.TrimSpace(b.String())
}

// ---------------------------------------------------------------------------
// Symbol extraction — powers the repo_map tool (Phase 6)
// ---------------------------------------------------------------------------

// TSExtractSymbols parses a file and extracts top-level symbol definitions.
func TSExtractSymbols(source []byte, path string) []Symbol {
	tree := TSParse(source, path)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	ext := filepath.Ext(path)
	root := tree.RootNode()
	lines := strings.Split(string(source), "\n")

	switch ext {
	case ".go":
		return extractGoSymbols(root, lines, source)
	case ".js", ".jsx", ".mjs", ".cjs":
		return extractJSTSSymbols(root, lines, source)
	case ".ts", ".tsx", ".mts", ".cts":
		return extractJSTSSymbols(root, lines, source)
	case ".py":
		return extractPythonSymbols(root, lines, source)
	default:
		return extractGenericSymbols(root, lines, source)
	}
}

func extractGoSymbols(root *tree_sitter.Node, lines []string, source []byte) []Symbol {
	var symbols []Symbol
	cursor := root.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return symbols
	}

	for {
		node := cursor.Node()
		kind := node.Kind()
		line := int(node.StartPosition().Row) + 1

		switch kind {
		case "function_declaration":
			name := nodeChildText(node, "name", source)
			sig := lineAt(lines, line-1)
			symbols = append(symbols, Symbol{Name: name, Kind: "function", Line: line, Signature: trimSig(sig)})

		case "method_declaration":
			name := nodeChildText(node, "name", source)
			receiver := nodeChildText(node, "receiver", source)
			sig := lineAt(lines, line-1)
			displayName := name
			if receiver != "" {
				displayName = receiver + "." + name
			}
			symbols = append(symbols, Symbol{Name: displayName, Kind: "method", Line: line, Signature: trimSig(sig)})

		case "type_declaration":
			specs := nodeChildrenByKind(node, "type_spec", cursor)
			for _, spec := range specs {
				tName := nodeChildText(&spec, "name", source)
				tKind := "type"
				typeNode := spec.ChildByFieldName("type")
				if typeNode != nil {
					switch typeNode.Kind() {
					case "interface_type":
						tKind = "interface"
					case "struct_type":
						tKind = "struct"
					}
				}
				specLine := int(spec.StartPosition().Row) + 1
				symbols = append(symbols, Symbol{Name: tName, Kind: tKind, Line: specLine, Signature: "type " + tName})
			}
		}

		if !cursor.GotoNextSibling() {
			break
		}
	}
	return symbols
}

func extractJSTSSymbols(root *tree_sitter.Node, lines []string, source []byte) []Symbol {
	var symbols []Symbol
	cursor := root.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return symbols
	}

	for {
		node := cursor.Node()
		kind := node.Kind()
		line := int(node.StartPosition().Row) + 1

		switch kind {
		case "function_declaration":
			name := nodeChildText(node, "name", source)
			symbols = append(symbols, Symbol{Name: name, Kind: "function", Line: line, Signature: trimSig(lineAt(lines, line-1))})

		case "class_declaration":
			name := nodeChildText(node, "name", source)
			symbols = append(symbols, Symbol{Name: name, Kind: "class", Line: line, Signature: trimSig(lineAt(lines, line-1))})

		case "interface_declaration":
			name := nodeChildText(node, "name", source)
			symbols = append(symbols, Symbol{Name: name, Kind: "interface", Line: line, Signature: trimSig(lineAt(lines, line-1))})

		case "type_alias_declaration":
			name := nodeChildText(node, "name", source)
			symbols = append(symbols, Symbol{Name: name, Kind: "type", Line: line, Signature: trimSig(lineAt(lines, line-1))})

		case "enum_declaration":
			name := nodeChildText(node, "name", source)
			symbols = append(symbols, Symbol{Name: name, Kind: "enum", Line: line, Signature: trimSig(lineAt(lines, line-1))})

		case "export_statement":
			decl := node.ChildByFieldName("declaration")
			if decl != nil {
				declKind := decl.Kind()
				name := nodeChildText(decl, "name", source)
				symKind := "variable"
				switch declKind {
				case "function_declaration":
					symKind = "function"
				case "class_declaration":
					symKind = "class"
				case "interface_declaration":
					symKind = "interface"
				case "type_alias_declaration":
					symKind = "type"
				case "enum_declaration":
					symKind = "enum"
				}
				if name != "" {
					symbols = append(symbols, Symbol{Name: name, Kind: symKind, Line: line, Signature: trimSig(lineAt(lines, line-1))})
				}
			}

		case "lexical_declaration":
			extractJSVarSymbols(node, lines, source, &symbols)
		}

		if !cursor.GotoNextSibling() {
			break
		}
	}
	return symbols
}

func extractJSVarSymbols(node *tree_sitter.Node, lines []string, source []byte, symbols *[]Symbol) {
	cursor := node.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return
	}
	for {
		child := cursor.Node()
		if child.Kind() == "variable_declarator" {
			name := nodeChildText(child, "name", source)
			value := child.ChildByFieldName("value")
			kind := "variable"
			if value != nil {
				vk := value.Kind()
				if vk == "arrow_function" || vk == "function_expression" || vk == "function" {
					kind = "function"
				}
			}
			line := int(child.StartPosition().Row) + 1
			if name != "" {
				*symbols = append(*symbols, Symbol{Name: name, Kind: kind, Line: line, Signature: trimSig(lineAt(lines, line-1))})
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
}

func extractPythonSymbols(root *tree_sitter.Node, lines []string, source []byte) []Symbol {
	var symbols []Symbol
	cursor := root.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return symbols
	}

	for {
		node := cursor.Node()
		kind := node.Kind()
		line := int(node.StartPosition().Row) + 1

		switch kind {
		case "function_definition":
			name := nodeChildText(node, "name", source)
			symbols = append(symbols, Symbol{Name: name, Kind: "function", Line: line, Signature: trimSig(lineAt(lines, line-1))})

		case "class_definition":
			name := nodeChildText(node, "name", source)
			symbols = append(symbols, Symbol{Name: name, Kind: "class", Line: line, Signature: trimSig(lineAt(lines, line-1))})

		case "decorated_definition":
			defNode := node.ChildByFieldName("definition")
			if defNode != nil {
				defKind := defNode.Kind()
				name := nodeChildText(defNode, "name", source)
				symKind := "function"
				if defKind == "class_definition" {
					symKind = "class"
				}
				symbols = append(symbols, Symbol{Name: name, Kind: symKind, Line: line, Signature: trimSig(lineAt(lines, line-1))})
			}
		}

		if !cursor.GotoNextSibling() {
			break
		}
	}
	return symbols
}

func extractGenericSymbols(root *tree_sitter.Node, lines []string, source []byte) []Symbol {
	var symbols []Symbol
	cursor := root.Walk()
	defer cursor.Close()

	if !cursor.GotoFirstChild() {
		return symbols
	}

	for {
		node := cursor.Node()
		if node.IsNamed() {
			kind := node.Kind()
			if strings.Contains(kind, "function") || strings.Contains(kind, "class") ||
				strings.Contains(kind, "method") || strings.Contains(kind, "type") {
				name := nodeChildText(node, "name", source)
				if name != "" {
					line := int(node.StartPosition().Row) + 1
					symbols = append(symbols, Symbol{Name: name, Kind: kind, Line: line, Signature: trimSig(lineAt(lines, line-1))})
				}
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return symbols
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func nodeChildText(node *tree_sitter.Node, fieldName string, source []byte) string {
	child := node.ChildByFieldName(fieldName)
	if child == nil {
		return ""
	}
	return child.Utf8Text(source)
}

func nodeChildrenByKind(node *tree_sitter.Node, kind string, parentCursor *tree_sitter.TreeCursor) []tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()

	var result []tree_sitter.Node
	if !cursor.GotoFirstChild() {
		return result
	}
	for {
		child := cursor.Node()
		if child.Kind() == kind {
			result = append(result, *child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return result
}

func lineAt(lines []string, idx int) string {
	if idx < 0 || idx >= len(lines) {
		return ""
	}
	return lines[idx]
}

func trimSig(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		s = s[:100] + "..."
	}
	if idx := strings.Index(s, "{"); idx > 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}
