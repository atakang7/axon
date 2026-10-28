package axon

import (
	"regexp"
	"strings"
)

// Automatic proof: when a goal changed files without recording criteria, the
// project's own tests can still prove it, but only when they evidently cover
// the change. "Some command passed" does not: replayed sessions showed users
// rejecting exactly those runs (GOALS.md). Two shapes do:
//
//   - red to green: a test command failed in this goal before the last write,
//     and a test command passes after it. The fix made the failing check pass.
//   - tests written: the goal changed a test file, and a test command passes
//     after the last write. The change came with its own check.
//
// Anything else still needs criteria. The eval of 2026-10-28 showed the
// ceremony costing 4 of 11 and 7 of 15 requests on tasks whose proof was
// exactly one of these shapes.

// testCommand matches commands that run a project's tests.
var testCommand = regexp.MustCompile(`(^|[\s;&|(])(go test|pytest|python3? -m (pytest|unittest)|npm (run )?test|pnpm (run )?test|yarn (run )?test|bun test|cargo test|(\./)?gradlew\S* .*\btest\w*|mvn .*\b(test|verify)|make (test|check)|jest|vitest|dotnet test|mix test|rspec|phpunit|ctest)\b`)

func isTestCommand(command string) bool {
	return testCommand.MatchString(normalizeCommand(command))
}

// testFile matches paths of test sources.
var testFile = regexp.MustCompile(`(?i)(_test\.go|_test\.py|(^|[/\\])test_[^/\\]*\.py|\.(test|spec)\.[jt]sx?|Test\.(java|kt)|Tests?\.cs|_spec\.rb)$|(^|[/\\])(tests?|__tests__|spec)[/\\]`)

// goalProof looks for automatic proof of a goal whose runs last wrote at
// lastWrite. It returns met with the passing run as evidence, or open.
func (s *Session) goalProof(goal string, lastWrite int, uses map[string]toolUse) critStatus {
	var pass string
	for i := lastWrite + 1; i < len(s.Messages); i++ {
		m := s.Messages[i]
		u := uses[m.ToolCallID]
		if m.Role == "tool" && !m.ToolFailed && u.name == toolExec && isTestCommand(argString(u.args, "command")) {
			if execExitCode(m.Content) == 0 {
				pass = m.ID
			} else {
				pass = "" // the latest test run decides
			}
		}
	}
	if pass == "" {
		return critStatus{state: critOpen}
	}
	red, wroteTest := false, false
	for _, r := range sessionRuns(s) {
		if s.goalOf(s.Messages[r.start].ID) != goal {
			continue
		}
		for i := r.start + 1; i < r.end && i <= lastWrite; i++ {
			m := s.Messages[i]
			if m.Role != "tool" || m.ToolFailed {
				continue
			}
			u := uses[m.ToolCallID]
			switch u.name {
			case toolExec:
				if isTestCommand(argString(u.args, "command")) && execExitCode(m.Content) != 0 {
					red = true
				}
			case toolWrite, toolEdit:
				for _, p := range writePaths(u) {
					if testFile.MatchString(p) {
						wroteTest = true
					}
				}
			}
		}
	}
	if red || wroteTest {
		return critStatus{state: critMet, evidence: pass}
	}
	return critStatus{state: critOpen}
}

// outputHas reports whether out contains want, ignoring differences in
// whitespace and case: agents write "ok example.com/x" for Go's
// "ok  \texample.com/x", and an exact match failed criteria that had passed.
func outputHas(out, want string) bool {
	if strings.Contains(out, want) {
		return true
	}
	squash := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	return strings.Contains(squash(out), squash(want))
}
