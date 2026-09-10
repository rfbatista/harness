package unit

import (
	"strings"
	"testing"

	"operators-mcp/internal/application/tooling"
)

// ---------------------------------------------------------------------------
// Bracket balance
// ---------------------------------------------------------------------------

func TestCheckBracketBalance_Balanced(t *testing.T) {
	content := `func main() {
	if true {
		doStuff(arr[0])
	}
}
`
	w := tooling.CheckBracketBalance(content)
	if w != "" {
		t.Errorf("expected no warnings, got: %s", w)
	}
}

func TestCheckBracketBalance_UnclosedBrace(t *testing.T) {
	content := `func main() {
	if true {
		doStuff()
}
`
	w := tooling.CheckBracketBalance(content)
	if w == "" {
		t.Error("expected warning for unclosed brace")
	}
}

func TestCheckBracketBalance_ExtraClosing(t *testing.T) {
	content := `func main() {
}
}
`
	w := tooling.CheckBracketBalance(content)
	if w == "" {
		t.Error("expected warning for extra closing brace")
	}
}

func TestCheckBracketBalance_IgnoresStrings(t *testing.T) {
	content := `s := "{ unbalanced ( bracket ["
t := '}'
`
	w := tooling.CheckBracketBalance(content)
	if w != "" {
		t.Errorf("brackets inside strings should be ignored, got: %s", w)
	}
}

// ---------------------------------------------------------------------------
// JavaScript checks
// ---------------------------------------------------------------------------

func TestCheckJSBasics_Clean(t *testing.T) {
	content := `import { useState } from 'react';

export function App() {
  const [count, setCount] = useState(0);
  return <button onClick={() => setCount(count + 1)}>{count}</button>;
}
`
	w := tooling.CheckJSBasics(content)
	if len(w) > 0 {
		t.Errorf("expected no warnings for clean JS, got: %v", w)
	}
}

func TestCheckJSBasics_Debugger(t *testing.T) {
	content := `function handle() {
  debugger;
  return 42;
}
`
	w := tooling.CheckJSBasics(content)
	found := false
	for _, s := range w {
		if strings.Contains(s, "debugger") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected debugger warning, got: %v", w)
	}
}

func TestCheckJSBasics_VarDeclaration(t *testing.T) {
	content := `var x = 1;
let y = 2;
const z = 3;
`
	w := tooling.CheckJSBasics(content)
	found := false
	for _, s := range w {
		if strings.Contains(s, "var") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected var warning, got: %v", w)
	}
}

func TestCheckJSBasics_DuplicateImport(t *testing.T) {
	content := `import { foo } from 'lodash';
import { bar } from 'lodash';
`
	w := tooling.CheckJSBasics(content)
	found := false
	for _, s := range w {
		if strings.Contains(s, "duplicate import") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate import warning, got: %v", w)
	}
}

func TestCheckJSBasics_UnclosedTemplateLiteral(t *testing.T) {
	content := "const s = `hello ${name}\n"
	w := tooling.CheckJSBasics(content)
	found := false
	for _, s := range w {
		if strings.Contains(s, "template literal") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected template literal warning, got: %v", w)
	}
}

func TestCheckJSBasics_BalancedTemplateLiteral(t *testing.T) {
	content := "const s = `hello ${name}`;\n"
	w := tooling.CheckJSBasics(content)
	for _, s := range w {
		if strings.Contains(s, "template literal") {
			t.Errorf("template literal should be balanced, got warning: %s", s)
		}
	}
}

// ---------------------------------------------------------------------------
// TypeScript checks
// ---------------------------------------------------------------------------

func TestCheckTSBasics_IncludesJSChecks(t *testing.T) {
	content := `var x = 1;
debugger;
`
	w := tooling.CheckTSBasics(content)
	hasVar := false
	hasDebugger := false
	for _, s := range w {
		if strings.Contains(s, "var") {
			hasVar = true
		}
		if strings.Contains(s, "debugger") {
			hasDebugger = true
		}
	}
	if !hasVar {
		t.Error("expected var warning from JS check")
	}
	if !hasDebugger {
		t.Error("expected debugger warning from JS check")
	}
}

func TestCheckTSBasics_ExcessiveAny(t *testing.T) {
	content := `function foo(a: any, b: any, c: any, d: any): any {
  return a;
}
`
	w := tooling.CheckTSBasics(content)
	found := false
	for _, s := range w {
		if strings.Contains(s, "any") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'any' type warning, got: %v", w)
	}
}

func TestCheckTSBasics_FewAny_NoWarning(t *testing.T) {
	content := `function foo(a: any): string {
  return String(a);
}
`
	w := tooling.CheckTSBasics(content)
	for _, s := range w {
		if strings.Contains(s, "any") {
			t.Errorf("should not warn on few 'any' usages, got: %s", s)
		}
	}
}

func TestCheckTSBasics_TSIgnore(t *testing.T) {
	content := `// @ts-ignore
const x: number = "not a number";
`
	w := tooling.CheckTSBasics(content)
	found := false
	for _, s := range w {
		if strings.Contains(s, "@ts-ignore") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @ts-ignore warning, got: %v", w)
	}
}

func TestCheckTSBasics_TSExpectError_NoWarning(t *testing.T) {
	content := `// @ts-expect-error intentional for test
const x: number = "not a number";
`
	w := tooling.CheckTSBasics(content)
	for _, s := range w {
		if strings.Contains(s, "@ts-ignore") {
			t.Errorf("@ts-expect-error should not trigger warning, got: %s", s)
		}
	}
}

func TestCheckTSBasics_DuplicateImport(t *testing.T) {
	content := `import { useEffect } from 'react';
import { useState } from 'react';
`
	w := tooling.CheckTSBasics(content)
	found := false
	for _, s := range w {
		if strings.Contains(s, "duplicate import") && strings.Contains(s, "react") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate import warning for react, got: %v", w)
	}
}
