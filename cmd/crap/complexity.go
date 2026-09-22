// Package main implements crap, a standalone CRAP (Change Risk Anti-Patterns)
// analyzer for Go, JavaScript, TypeScript and Python.
package main

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

// FunctionInfo is one analyzed function or method: its location and
// cyclomatic complexity, before any coverage data is attached.
type FunctionInfo struct {
	File       string
	Name       string
	StartLine  int
	EndLine    int
	Complexity int
}

// langSpec describes, for one language's tree-sitter grammar, which node
// kinds introduce a function, a naming scope (class/struct), a branch that
// adds to cyclomatic complexity, and which binary-operator node kinds add a
// branch only for specific operators (&&, ||, and, or, ...).
type langSpec struct {
	funcKinds   map[string]bool
	classKinds  map[string]bool
	branchKinds map[string]bool
	boolOpKinds map[string]bool
	boolOps     map[string]bool
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

var goSpec = &langSpec{
	funcKinds:   set("function_declaration", "method_declaration", "func_literal"),
	branchKinds: set("if_statement", "for_statement", "expression_case", "type_case", "communication_case"),
	boolOpKinds: set("binary_expression"),
	boolOps:     set("&&", "||"),
}

var jsSpec = &langSpec{
	funcKinds:   set("function_declaration", "function_expression", "generator_function_declaration", "generator_function", "arrow_function", "method_definition"),
	classKinds:  set("class_declaration"),
	branchKinds: set("if_statement", "for_statement", "for_in_statement", "while_statement", "do_statement", "switch_case", "catch_clause", "ternary_expression"),
	boolOpKinds: set("binary_expression"),
	boolOps:     set("&&", "||", "??"),
}

var pySpec = &langSpec{
	funcKinds:   set("function_definition"),
	classKinds:  set("class_definition"),
	branchKinds: set("if_statement", "elif_clause", "for_statement", "while_statement", "except_clause", "conditional_expression", "case_clause"),
	boolOpKinds: set("boolean_operator"),
	boolOps:     set("and", "or"),
}

// extLanguage maps a file extension to its tree-sitter grammar constructor.
var extLanguage = map[string]func() unsafe.Pointer{
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

// extSpec maps a file extension to its branch/function node-kind spec.
// TypeScript and TSX reuse jsSpec: the statement grammar they inherit from
// JavaScript is unchanged.
var extSpec = map[string]*langSpec{
	".go":  goSpec,
	".js":  jsSpec,
	".jsx": jsSpec,
	".mjs": jsSpec,
	".cjs": jsSpec,
	".ts":  jsSpec,
	".mts": jsSpec,
	".cts": jsSpec,
	".tsx": jsSpec,
	".py":  pySpec,
}

// SupportsExt reports whether crap can analyze files with the given extension.
func SupportsExt(ext string) bool {
	_, ok := extLanguage[ext]
	return ok
}

// AnalyzeFile parses source with the grammar matching path's extension and
// returns one FunctionInfo per function/method found (at any nesting depth).
// It returns (nil, nil) for unsupported extensions.
func AnalyzeFile(path string, source []byte) ([]FunctionInfo, error) {
	ext := strings.ToLower(filepath.Ext(path))
	langFn, ok := extLanguage[ext]
	if !ok {
		return nil, nil
	}
	spec := extSpec[ext]

	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(langFn())); err != nil {
		return nil, fmt.Errorf("set language for %s: %w", path, err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s: no tree produced", path)
	}
	defer tree.Close()

	var funcs []FunctionInfo
	collectFunctions(tree.RootNode(), spec, source, "", path, &funcs)
	return funcs, nil
}

// collectFunctions walks the whole tree (not just top level) so nested
// methods, closures and functions defined inside classes are each reported
// as their own FunctionInfo. prefix carries the enclosing class/struct name
// (e.g. "Foo.") down into its methods.
func collectFunctions(node *tree_sitter.Node, spec *langSpec, source []byte, prefix, path string, out *[]FunctionInfo) {
	kind := node.Kind()

	childPrefix := prefix
	if spec.classKinds[kind] {
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			childPrefix = nameNode.Utf8Text(source) + "."
		}
	}

	if spec.funcKinds[kind] {
		*out = append(*out, FunctionInfo{
			File:       path,
			Name:       functionName(node, kind, source, prefix),
			StartLine:  int(node.StartPosition().Row) + 1,
			EndLine:    int(node.EndPosition().Row) + 1,
			Complexity: 1 + countDecisions(node, spec, source, true),
		})
	}

	n := node.NamedChildCount()
	for i := uint(0); i < n; i++ {
		if child := node.NamedChild(i); child != nil {
			collectFunctions(child, spec, source, childPrefix, path, out)
		}
	}
}

// countDecisions sums cyclomatic-complexity branch points within node,
// excluding the bodies of any nested function-like node (those are counted
// separately, against themselves, by collectFunctions).
func countDecisions(node *tree_sitter.Node, spec *langSpec, source []byte, isRoot bool) int {
	kind := node.Kind()
	if !isRoot && spec.funcKinds[kind] {
		return 0
	}

	count := 0
	if spec.branchKinds[kind] {
		count++
	}
	if spec.boolOpKinds[kind] {
		if opNode := node.ChildByFieldName("operator"); opNode != nil && spec.boolOps[opNode.Utf8Text(source)] {
			count++
		}
	}

	n := node.NamedChildCount()
	for i := uint(0); i < n; i++ {
		if child := node.NamedChild(i); child != nil {
			count += countDecisions(child, spec, source, false)
		}
	}
	return count
}

// functionName resolves a function-like node's display name: its declared
// name (qualified by receiver type for a Go method, or by enclosing class
// for JS/Python), or a "<anonymous>:LINE" fallback for arrow functions,
// function expressions and Go func literals.
func functionName(node *tree_sitter.Node, kind string, source []byte, prefix string) string {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		name := nameNode.Utf8Text(source)
		if kind == "method_declaration" {
			if recv := goReceiverType(node, source); recv != "" {
				return "(" + recv + ")." + name
			}
		}
		return prefix + name
	}
	return fmt.Sprintf("%s<anonymous>:%d", prefix, int(node.StartPosition().Row)+1)
}

// goReceiverType extracts the receiver type name from a Go method_declaration
// node, e.g. "*Foo" -> "Foo", "Foo" -> "Foo".
func goReceiverType(node *tree_sitter.Node, source []byte) string {
	recv := node.ChildByFieldName("receiver")
	if recv == nil {
		return ""
	}
	n := recv.NamedChildCount()
	for i := uint(0); i < n; i++ {
		pd := recv.NamedChild(i)
		if pd == nil || pd.Kind() != "parameter_declaration" {
			continue
		}
		t := pd.ChildByFieldName("type")
		if t == nil {
			continue
		}
		return strings.TrimPrefix(t.Utf8Text(source), "*")
	}
	return ""
}
