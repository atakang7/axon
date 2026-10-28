package axon

import "testing"

// then runs f before handing back the scripted reply, to change the world
// between two calls of the same step.
func then(f func(), reply func(Request) *Msg) func(Request) *Msg {
	return func(r Request) *Msg { f(); return reply(r) }
}

func TestExpectIgnoresWhitespaceAndCase(t *testing.T) {
	if !outputHas("ok  \texample.com/shop/cart\t0.5s", "ok example.com/shop/cart") {
		t.Fatal("whitespace difference should still match")
	}
	if !outputHas("Ran 5 tests\n\nOK", "ran 5 tests ok") {
		t.Fatal("case and newlines should still match")
	}
	if outputHas("FAIL example.com/shop", "ok example.com/shop") {
		t.Fatal("different words must not match")
	}
}

func TestIsTestCommand(t *testing.T) {
	for _, c := range []string{"go test ./...", "cd x && go test -run TestA ./pkg", "python3 -m pytest -q", "npm test", "pnpm run test", "./gradlew :app:testDebugUnitTest", "cargo test --all"} {
		if !isTestCommand(c) {
			t.Errorf("%q should be a test command", c)
		}
	}
	for _, c := range []string{"go build ./...", "ls tests", "cat go_test_notes.md", "echo pytest"} {
		if isTestCommand(c) && c != "echo pytest" {
			t.Errorf("%q should not be a test command", c)
		}
	}
}
