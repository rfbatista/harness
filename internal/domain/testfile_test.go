package domain

import "testing"

func TestTheTestFileRuleClassifiesTestAndNonTestPathsForTheCommonStacks(t *testing.T) {
	tests := []string{
		"internal/app/server_test.go", "web/src/a.test.js", "web/src/a.spec.tsx", "web/src/__tests__/a.js",
		"tests/test_api.py", "pkg/test_util.py", "pkg/util_test.py", "pkg/conftest.py", "spec/models/user_spec.rb",
		"src/test/java/com/x/FooTest.java", "src/test/kotlin/FooTests.kt", "tests/integration.rs", "test/helper.rb",
		"internal/x/testdata/golden.json", "web/fixtures/a.json", "web/src/__snapshots__/a.snap", "Foo.Tests/BarTests.cs",
	}
	for _, p := range tests {
		if !IsTestFile(p) {
			t.Errorf("%q is a test file", p)
		}
	}
	nonTests := []string{
		"main.go", "internal/app/server.go", "web/src/a.js", "pkg/util.py", "src/main/java/com/x/Foo.java", "README.md",
		"internal/latest/readme.md", "contest.py", "attestation.go", "go.mod", "src/protest.ts",
	}
	for _, p := range nonTests {
		if IsTestFile(p) {
			t.Errorf("%q is not a test file", p)
		}
	}
}
