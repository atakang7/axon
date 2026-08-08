package axon

import "testing"

// Every declared Kind must have a name, and every name must round-trip to a
// distinct Kind — otherwise two event kinds would print identically in a
// trace, silently merging in whatever log or dashboard reads Kind.String().
func TestKindStringCoversEveryDeclaredKind(t *testing.T) {
	seen := map[string]Kind{}
	for k := KindUnknown; k <= KindSessionEnd; k++ {
		name := k.String()
		if name == "" {
			t.Fatalf("Kind(%d) has no name", int(k))
		}
		if other, ok := seen[name]; ok {
			t.Fatalf("Kind(%d) and Kind(%d) both name themselves %q", int(k), int(other), name)
		}
		seen[name] = k
	}
}

func TestKindStringOutOfRangeDoesNotPanic(t *testing.T) {
	if got := Kind(-1).String(); got == "" {
		t.Fatal("Kind(-1).String() = \"\", want a fallback")
	}
	if got := Kind(9999).String(); got == "" {
		t.Fatal("Kind(9999).String() = \"\", want a fallback")
	}
}
