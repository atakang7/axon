package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func calls(names ...string) []ToolCall {
	out := make([]ToolCall, len(names))
	for i, n := range names {
		out[i].ID = fmt.Sprintf("c%d", i)
		out[i].Function.Name = n
		out[i].Function.Arguments = "{}"
	}
	return out
}

// A write between reads is a barrier: "write X, then read X" in one reply must
// still read the new contents.
func TestToolGroupsBatchReadsButBarrierAtMutations(t *testing.T) {
	groups := toolGroups(calls("read", "search", "write", "read", "exec", "exec"))
	var sizes []int
	for _, g := range groups {
		sizes = append(sizes, len(g))
	}
	if fmt.Sprint(sizes) != "[2 1 1 1 1]" {
		t.Fatalf("group sizes %v", sizes)
	}
}

func TestReadOnlyCallsRunConcurrentlyAndKeepOrder(t *testing.T) {
	cfg := noToolsConfig(t)
	started := make(chan string, 2)
	release := make(chan struct{})
	wait := func(name string) func(context.Context, json.RawMessage) (string, error) {
		return func(context.Context, json.RawMessage) (string, error) {
			started <- name
			select {
			case <-release:
			case <-time.After(5 * time.Second):
				return "", fmt.Errorf("%s ran alone: calls were serialized", name)
			}
			return name + "-result", nil
		}
	}
	cfg.Tools = []Tool{fnTool("read", wait("read")), fnTool("search", wait("search"))}
	go func() {
		<-started
		<-started
		close(release)
	}()
	cfg.Model = newQueueModel(scriptedReply{msg: &Msg{Role: "assistant", ToolCalls: calls("read", "search")}}, scriptedReply{msg: finalMsg("ok")})
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, m := range a.session.Messages {
		if m.Role == "tool" {
			results = append(results, m.Content)
		}
	}
	if len(results) != 2 || !strings.HasSuffix(results[0], "read-result") || !strings.HasSuffix(results[1], "search-result") {
		t.Fatalf("results out of call order: %q", results)
	}
}

func TestDripLookupsGetOneEphemeralPlanningNudge(t *testing.T) {
	cfg := noToolsConfig(t)
	cfg.Tools = []Tool{fnTool("read", func(context.Context, json.RawMessage) (string, error) { return "contents", nil })}
	model := newQueueModel(
		scriptedReply{msg: toolCallMsg("a", "read", `{"path":"a.go"}`)},
		scriptedReply{msg: toolCallMsg("b", "read", `{"path":"b.go"}`)},
		scriptedReply{msg: toolCallMsg("c", "read", `{"path":"c.go"}`)},
		scriptedReply{msg: finalMsg("done")},
	)
	cfg.Model = model
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	reqs := model.reqs()
	if len(reqs) != 4 {
		t.Fatalf("%d requests", len(reqs))
	}
	nudged := func(r Request) bool {
		return strings.Contains(r.Messages[len(r.Messages)-1].Content, "runtime note")
	}
	if nudged(reqs[0]) || nudged(reqs[1]) || !nudged(reqs[2]) || nudged(reqs[3]) {
		t.Fatalf("nudge pattern wrong: %v %v %v %v", nudged(reqs[0]), nudged(reqs[1]), nudged(reqs[2]), nudged(reqs[3]))
	}
	for _, m := range a.session.Messages {
		if strings.Contains(m.Content, "runtime note") {
			t.Fatal("nudge was persisted into the session")
		}
	}
}

func TestBatchedLookupsAreNotDrip(t *testing.T) {
	var g gatherTracker
	batch := calls("read")
	batch[0].Function.Arguments = `{"files":[{"path":"a.go"},{"path":"b.go"}]}`
	for range 5 {
		if g.observe(batch) != 0 || g.observe(calls("read", "search")) != 0 {
			t.Fatal("batched lookups counted as drip")
		}
	}
}

func TestReadFilesBatchReturnsEveryFileAndReportsFailuresInPlace(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nbeta\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	tool := ReadTool(newFakeWorkspace(dir), testLimits())
	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"files": []map[string]any{
		{"path": "a.txt", "mode": "full"},
		{"path": "missing.txt"},
		{"path": "b.txt", "offset": 2, "limit": 1},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"=== a.txt ===", "alpha", "=== missing.txt ===", "[error:", "=== b.txt (from line 2) ===", "2\ttwo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("batch read missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "three") {
		t.Fatalf("per-file limit ignored:\n%s", out)
	}
	if _, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "a.txt", "files": []map[string]any{{"path": "b.txt"}}})); err == nil {
		t.Fatal("path and files together accepted")
	}
}
