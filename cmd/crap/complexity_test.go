package main

import "testing"

func complexityOf(t *testing.T, funcs []FunctionInfo, name string) int {
	t.Helper()
	for _, f := range funcs {
		if f.Name == name {
			return f.Complexity
		}
	}
	names := make([]string, 0, len(funcs))
	for _, f := range funcs {
		names = append(names, f.Name)
	}
	t.Fatalf("function %q not found; got %v", name, names)
	return 0
}

func TestAnalyzeFile_Go(t *testing.T) {
	src := []byte(`package sample

func Simple() int {
	return 1
}

func Branchy(a, b int) int {
	if a > 0 && b > 0 {
		return 1
	} else if a < 0 {
		return -1
	}
	for i := 0; i < 10; i++ {
		if i == 5 {
			continue
		}
	}
	switch a {
	case 1:
		return 1
	case 2:
		return 2
	default:
		return 0
	}
	return 0
}

type T struct{}

func (t *T) Method() int {
	f := func() int { return 42 }
	return f()
}
`)
	funcs, err := AnalyzeFile("sample.go", src)
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}

	if got := complexityOf(t, funcs, "Simple"); got != 1 {
		t.Errorf("Simple complexity = %d, want 1", got)
	}
	// if(+1) && (+1) + else-if(+1) + for(+1) + nested if(+1) + 2 case clauses(+2) = 7 -> complexity 8
	if got := complexityOf(t, funcs, "Branchy"); got != 8 {
		t.Errorf("Branchy complexity = %d, want 8", got)
	}
	if got := complexityOf(t, funcs, "(T).Method"); got != 1 {
		t.Errorf("(T).Method complexity = %d, want 1 (nested func literal must not inflate it)", got)
	}
	// the nested func literal must be reported as its own function, not merged into Method.
	found := false
	for _, f := range funcs {
		if f.File == "sample.go" && f.Name == "<anonymous>:32" {
			found = true
			if f.Complexity != 1 {
				t.Errorf("nested literal complexity = %d, want 1", f.Complexity)
			}
		}
	}
	if !found {
		t.Errorf("expected a separate FunctionInfo for the nested func literal; got %+v", funcs)
	}
}

func TestAnalyzeFile_JavaScript(t *testing.T) {
	src := []byte(`function branchy(a, b) {
  if (a > 0 && b > 0) {
    return 1;
  } else if (a < 0) {
    return -1;
  }
  for (let i = 0; i < 10; i++) {
    while (i > 5) {
      break;
    }
  }
  switch (a) {
    case 1:
      return 1;
    case 2:
      return 2;
    default:
      return 0;
  }
  try {
  } catch (e) {
  }
  return a ? 1 : 0;
}
`)
	funcs, err := AnalyzeFile("sample.js", src)
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	// if(+1)&&(+1) + else-if(+1) + for(+1) + while(+1) + 2 switch_case(+2)
	// + catch_clause(+1) + ternary(+1) = 9 -> complexity 10
	if got := complexityOf(t, funcs, "branchy"); got != 10 {
		t.Errorf("branchy complexity = %d, want 10", got)
	}
}

func TestAnalyzeFile_TypeScriptClassMethod(t *testing.T) {
	src := []byte(`class Widget {
  render(active: boolean): number {
    if (active) {
      return 1;
    }
    return 0;
  }
}
`)
	funcs, err := AnalyzeFile("sample.ts", src)
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	if got := complexityOf(t, funcs, "Widget.render"); got != 2 {
		t.Errorf("Widget.render complexity = %d, want 2", got)
	}
}

func TestAnalyzeFile_Python(t *testing.T) {
	src := []byte(`def branchy(a, b):
    if a > 0 and b > 0:
        return 1
    elif a < 0:
        return -1
    for i in range(10):
        while i > 5:
            break
    if a == 1:
        pass
    try:
        pass
    except ValueError:
        pass
    except TypeError:
        pass
    return 1 if a else 0
`)
	funcs, err := AnalyzeFile("sample.py", src)
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	// if(+1)and(+1) + elif_clause(+1) + for(+1) + while(+1) + if(+1)
	// + 2 except_clause(+2) + conditional_expression(+1) = 9 -> complexity 10
	if got := complexityOf(t, funcs, "branchy"); got != 10 {
		t.Errorf("branchy complexity = %d, want 10", got)
	}
}

func TestAnalyzeFile_UnsupportedExtension(t *testing.T) {
	funcs, err := AnalyzeFile("README.md", []byte("# hi"))
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	if funcs != nil {
		t.Errorf("expected nil for unsupported extension, got %v", funcs)
	}
}

func TestGoReceiverType(t *testing.T) {
	src := []byte(`package sample

type Foo struct{}

func (f *Foo) Pointer() {}
func (f Foo) Value()    {}
`)
	funcs, err := AnalyzeFile("sample.go", src)
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	complexityOf(t, funcs, "(Foo).Pointer")
	complexityOf(t, funcs, "(Foo).Value")
}
