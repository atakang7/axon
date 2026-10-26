package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Exploration: a cheaper model answers a read-only question in its own
// throw-away conversation and reports facts with quotes; code checks every
// quote against the file before anything reaches the deciding model. The
// explorer never decides anything. See WORKING_CONTEXT.md.

const (
	toolExplore = "explore"
	toolReport  = "report"
)

// exploreInputLimit bounds one explorer request; exploreWrapUp is the size at
// which it is told to report with what it has. Its context is thrown away
// afterwards, so it may grow well past the main agent's budget.
const (
	exploreInputLimit = 120000
	exploreWrapUp     = 90000
)

// Caps on what one report may deliver.
const (
	maxExploreFacts    = 40
	maxExploreNotFound = 10
	maxQuoteRunes      = 600
	maxClaimRunes      = 240
	maxExploreFileSize = 4 << 20
)

const exploreDescription = `Ask a cheaper, read-only model to find where things are. It searches and reads in its own context and returns facts, each with an exact quote that was checked against the file, plus what it could not find. Use it for broad questions before you open what you will change: which files define X, who calls Y, how Z is configured, what a directory holds. It cannot write, run commands or recommend anything; deciding stays with you, and you still read yourself whatever you will edit. Independent questions can go out as parallel calls. "Not found" is not proof of absence.`

const explorerPrompt = `You are a read-only code explorer. An engineer who makes every decision asked you a question about this repository. Find the answer with the read and search tools, then deliver it with the report tool.

How to work:
- Search before you read. Read only the parts you need, several files in one call when you can.
- You have at most %d rounds. Stop as soon as the question is answered.
- Call report once, at the end. Nothing you write outside report is delivered.

What to report:
- Facts only: what exists, where it is, what it does. Never what should be changed, never a plan or advice.
- Every fact cites a path, a line range and a quote: one to three lines copied exactly from that file, without the line numbers the read tool shows. A fact whose quote is not in the file is discarded.
- claim: one short sentence on what the quoted code means for the question.
- What the question asks about but you could not find goes in not_found, with where you searched.`

// exploreTool is bound to the agent: it uses the agent's workspace, limits and
// explorer model, and adds the explorer's spend to the session.
func (a *Agent) exploreTool() Tool {
	return Tool{
		Name:        toolExplore,
		Description: exploreDescription,
		Schema: obj("object", props{
			"question": strSchema("What to find out, as a specific question."),
			"paths":    arr(strSchema("A file or directory to start from.")),
		}, []string{"question"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in struct {
				Question string   `json:"question"`
				Paths    []string `json:"paths"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", err
			}
			if strings.TrimSpace(in.Question) == "" {
				return "", errors.New("question is required")
			}
			return a.explore(ctx, strings.TrimSpace(in.Question), in.Paths)
		},
	}
}

// exploreReport is the report tool's argument.
type exploreReport struct {
	Facts    []exploreFact `json:"facts"`
	NotFound []struct {
		What     string `json:"what"`
		Searched string `json:"searched"`
	} `json:"not_found"`
}

type exploreFact struct {
	Path      string  `json:"path"`
	StartLine flexInt `json:"start_line"`
	EndLine   flexInt `json:"end_line"`
	Quote     string  `json:"quote"`
	Claim     string  `json:"claim"`
}

// flexInt accepts 12, "12" and "12-14" (the start), which cheaper models
// produce interchangeably for a line number.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if i := strings.IndexAny(s, "-:,"); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("line %s is not a number", b)
	}
	*f = flexInt(n)
	return nil
}

var reportSchema = obj("object", props{
	"facts": arr(obj("object", props{
		"path":       strSchema("File path, relative to the repository root."),
		"start_line": intSchema("First line of the quote."),
		"end_line":   intSchema("Last line of the quote."),
		"quote":      strSchema("One to three lines copied exactly from the file, without line numbers."),
		"claim":      strSchema("One short sentence: what this code means for the question."),
	}, []string{"path", "start_line", "quote", "claim"})),
	"not_found": arr(obj("object", props{
		"what":     strSchema("What the question asks about that you could not find."),
		"searched": strSchema("Where you looked."),
	}, []string{"what", "searched"})),
}, []string{"facts"})

// explore runs one exploration to its report and returns what the deciding
// model sees: verified facts only.
//
// It runs inside a tool call, possibly next to other explore calls, so it
// emits only through progress, which serializes: the event sink is not
// required to be goroutine-safe.
func (a *Agent) explore(ctx context.Context, question string, paths []string) (string, error) {
	pool := a.explorers
	if len(pool) == 0 {
		return "", errors.New("no explorer model is configured; read and search yourself")
	}
	if a.exploreFailed.Load() {
		return "", errors.New("every explorer already failed in this request; read and search yourself")
	}
	// Each call starts at the next model, so parallel questions spread over
	// the pool; a model that fails or does not report hands the question on.
	start := int(a.exploreNext.Add(1)-1) % len(pool)
	var failures []string
	var partial *exploreStopped
	for i := range pool {
		model := pool[(start+i)%len(pool)]
		label := modelName(model)
		if label == "" {
			label = fmt.Sprintf("explorer %d", (start+i)%len(pool)+1)
		}
		a.progress(ctx, toolExplore, label+" is exploring")
		out, err := a.exploreWith(ctx, model, question, paths)
		if err != nil && ctx.Err() == nil {
			a.progress(ctx, toolExplore, label+" stopped: "+oneLine(err.Error(), 120))
		}
		if err == nil {
			if len(failures) > 0 {
				out += "\n(" + strings.Join(failures, "; ") + ")"
			}
			return out, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		failures = append(failures, label+" failed: "+oneLine(err.Error(), 160))
		var stopped *exploreStopped
		if errors.As(err, &stopped) && (partial == nil || len(stopped.looked) > len(partial.looked)) {
			partial = stopped
		}
	}
	a.exploreFailed.Store(true)
	if partial != nil && len(partial.looked) > 0 {
		// The work is not thrown away: where the explorer looked is a fact,
		// and it spares the deciding model locating the same places again.
		var b strings.Builder
		fmt.Fprintf(&b, "explore: %q\nEvery explorer stopped before reporting (%s), so no fact was checked. "+
			"Where the furthest one looked, in order:", question, strings.Join(failures, "; "))
		for _, l := range partial.looked {
			b.WriteString("\n- " + l)
		}
		b.WriteString("\nStart from these; do not explore this question again.")
		return b.String(), nil
	}
	return "", fmt.Errorf("every explorer failed (%s); read and search yourself, and do not call explore again in this request", strings.Join(failures, "; "))
}

// explorerRetry is the explorer's retry policy. Free models rate-limit often;
// a model that is busy hands the question to the next one in the pool after
// one short wait instead of spending minutes on backoff.
var explorerRetry = RetryConfig{MaxAttempts: 2, BackoffCap: Duration(10 * time.Second)}

// exploreStopped is an exploration that ended without a report, with where
// it had looked.
type exploreStopped struct {
	err    error
	looked []string
}

func (e *exploreStopped) Error() string { return e.err.Error() }
func (e *exploreStopped) Unwrap() error { return e.err }

// exploreWith runs one exploration with one model to its report.
func (a *Agent) exploreWith(ctx context.Context, model Model, question string, paths []string) (string, error) {
	cfg := a.settings.Explorer
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout.Std())
	defer cancel()

	ws := a.session
	var report *exploreReport
	reportTool := Tool{
		Name:        toolReport,
		Description: "Deliver your findings. Call once, at the end.",
		Schema:      reportSchema,
		Fn: func(_ context.Context, raw json.RawMessage) (string, error) {
			var r exploreReport
			if err := json.Unmarshal(raw, &r); err != nil {
				return "", fmt.Errorf("report rejected: %v; send it again with the documented fields", err)
			}
			report = &r
			return "Report received.", nil
		},
	}
	tools := []Tool{ReadTool(ws, a.limits), SearchTool(ws, a.limits), reportTool}

	task := "Repository: " + ws.Dir() + "\nQuestion: " + question
	if len(paths) > 0 {
		task += "\nStart from: " + strings.Join(paths, ", ")
	}
	msgs := []Msg{
		{Role: "system", Content: fmt.Sprintf(explorerPrompt, cfg.MaxRounds)},
		{Role: "user", Content: task},
	}
	usage := Stream{Usage: func(prompt, completion int, cost float64) {
		a.usageMu.Lock()
		a.session.ExplorerTokens += prompt + completion
		a.session.Cost += cost
		a.usageMu.Unlock()
	}}

	rounds, nudged, prose := 0, false, ""
	var looked []string
	for rounds <= cfg.MaxRounds && report == nil {
		rounds++
		toolset := tools
		size, _ := requestInputTokens(Request{Messages: msgs, Tools: toolSpecs(tools)}, modelName(model))
		final := rounds > cfg.MaxRounds || size > exploreWrapUp
		if final {
			msgs = append(msgs, Msg{Role: "user", Content: "Your exploration budget is used up. Call report now with the facts you have; no other tool is available."})
			toolset = []Tool{reportTool}
		}
		reply, err := a.completeWith(ctx, model, Request{
			Messages:   msgs,
			Tools:      toolSpecs(toolset),
			MaxTokens:  cfg.MaxTokens,
			InputLimit: exploreInputLimit,
		}, usage, true, explorerRetry)
		if err != nil {
			return "", &exploreStopped{err: fmt.Errorf("explorer failed after %d rounds: %v", rounds, err), looked: looked}
		}
		msgs = append(msgs, Msg{Role: "assistant", Content: reply.Content, ToolCalls: reply.ToolCalls})
		if len(strings.TrimSpace(reply.Content)) > len(strings.TrimSpace(prose)) {
			prose = reply.Content
		}
		if len(reply.ToolCalls) == 0 {
			if nudged || final {
				break
			}
			nudged = true
			msgs = append(msgs, Msg{Role: "user", Content: "Call the report tool with your facts; text outside it is not delivered."})
			continue
		}
		for _, tc := range reply.ToolCalls {
			out := runExplorerTool(ctx, toolset, tc)
			msgs = append(msgs, Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: out})
			failed := strings.HasPrefix(out, "error:")
			if tc.Function.Name != toolReport && !failed {
				looked = append(looked, tc.Function.Name+" "+oneLine(tc.Function.Arguments, 160))
			}
			step := exploreStep(tc)
			if failed {
				step += " (failed)"
			}
			a.progress(ctx, toolExplore, step)
		}
		if final {
			break
		}
	}
	if report == nil {
		// Weaker models sometimes write their findings as prose and never call
		// report. The prose itself is never passed on; only quotes in it that
		// are found verbatim in a file it names survive, checked like any fact.
		if facts, _ := verifyFacts(ws, factsFromProse(ws, prose)); len(facts) > 0 {
			return formatExplore(question, facts, nil, &exploreReport{}, modelName(model), rounds,
				"The explorer answered in prose instead of calling report; only the quotes below were recovered from it, each found in the file."), nil
		}
		return "", &exploreStopped{err: fmt.Errorf("the explorer did not report after %d rounds", rounds), looked: looked}
	}
	facts, rejected := verifyFacts(ws, report.Facts)
	return formatExplore(question, facts, rejected, report, modelName(model), rounds, ""), nil
}

var (
	prosePath  = regexp.MustCompile("[A-Za-z0-9_./\\\\-]+\\.[A-Za-z0-9]{1,8}")
	proseFence = regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)```")
	proseSpan  = regexp.MustCompile("`([^`\\n]+)`")
)

// factsFromProse turns an explorer's prose into candidate facts: every quote
// it put in code (a fenced line or an inline span) that is found in one of the
// files it names. The claim is the prose line the quote sat in. verifyFacts
// checks them again like any reported fact.
func factsFromProse(ws Workspace, prose string) []exploreFact {
	if strings.TrimSpace(prose) == "" {
		return nil
	}
	var files []string
	seen := map[string]bool{}
	for _, p := range prosePath.FindAllString(prose, -1) {
		p = strings.Trim(p, "./")
		if p == "" || seen[p] || len(files) >= 20 {
			continue
		}
		seen[p] = true
		if info, err := os.Stat(ws.ResolvePath(p)); err == nil && !info.IsDir() && info.Size() <= maxExploreFileSize {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return nil
	}
	type snippet struct{ quote, line string }
	var snippets []snippet
	lineOf := func(at int) string {
		start := strings.LastIndex(prose[:at], "\n") + 1
		end := strings.Index(prose[at:], "\n")
		if end < 0 {
			end = len(prose) - at
		}
		return strings.ReplaceAll(prose[start:at+end], "`", "")
	}
	for _, m := range proseFence.FindAllStringSubmatchIndex(prose, -1) {
		// A fenced block's claim is the prose line just above it.
		above := strings.TrimRight(prose[:m[0]], " \t\n")
		before := strings.ReplaceAll(above[strings.LastIndex(above, "\n")+1:], "`", "")
		for _, l := range strings.Split(prose[m[2]:m[3]], "\n") {
			snippets = append(snippets, snippet{lineGutter.ReplaceAllString(l, ""), before})
		}
	}
	for _, m := range proseSpan.FindAllStringSubmatchIndex(prose, -1) {
		snippets = append(snippets, snippet{prose[m[2]:m[3]], lineOf(m[0])})
	}
	texts := map[string]string{}
	var out []exploreFact
	for _, sn := range snippets {
		q := strings.TrimSpace(sn.quote)
		if len(strings.Join(strings.Fields(q), "")) < 6 || seen[q] || len(out) >= maxExploreFacts {
			continue
		}
		for _, f := range files {
			text, ok := texts[f]
			if !ok {
				data, err := readSourceFile(ws.ResolvePath(f))
				if err != nil {
					continue
				}
				text = strings.ReplaceAll(string(data), "\r\n", "\n")
				texts[f] = text
			}
			if quoteIndex(text, q) != nil {
				seen[q] = true
				out = append(out, exploreFact{Path: f, Quote: q, Claim: oneLine(sn.line, maxClaimRunes)})
				break
			}
		}
	}
	return out
}

// runExplorerTool runs one of the explorer's calls. Only the tools offered in
// this round can run, so a call to anything else, including the deciding
// model's write and exec, is refused.
func runExplorerTool(ctx context.Context, tools []Tool, tc ToolCall) string {
	for _, t := range tools {
		if t.Name != tc.Function.Name {
			continue
		}
		out, err := t.Fn(ctx, json.RawMessage(tc.Function.Arguments))
		if err != nil {
			return "error: " + err.Error()
		}
		return out
	}
	return fmt.Sprintf("error: %q is not available; you can use %s", tc.Function.Name, toolNames(tools))
}

// exploreStep is one explorer call as a short line for the user: what it read
// or searched, or what it reported.
func exploreStep(tc ToolCall) string {
	var in struct {
		Path    string `json:"path"`
		Query   string `json:"query"`
		Literal string `json:"literal"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
		Files   []struct {
			Path string `json:"path"`
		} `json:"files"`
		Facts []json.RawMessage `json:"facts"`
	}
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &in)
	switch tc.Function.Name {
	case toolRead:
		var paths []string
		if in.Path != "" {
			p := in.Path
			if in.Offset > 0 || in.Limit > 0 {
				p += fmt.Sprintf(" (from line %d)", max(in.Offset, 1))
			}
			paths = append(paths, p)
		}
		for _, f := range in.Files {
			paths = append(paths, f.Path)
		}
		return "Read " + oneLine(strings.Join(paths, ", "), 160)
	case toolSearch:
		q := in.Query
		if q == "" {
			q = in.Literal
		}
		s := "Search " + strconv.Quote(oneLine(q, 80))
		if in.Path != "" {
			s += " in " + in.Path
		}
		return s
	case toolReport:
		return fmt.Sprintf("Reported %d facts", len(in.Facts))
	}
	return tc.Function.Name + " " + oneLine(tc.Function.Arguments, 120)
}

func toolNames(tools []Tool) string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// verifiedFact is a fact whose quote was found in the file it cites.
type verifiedFact struct {
	path       string
	start, end int
	source     string // the file's full lines the quote spans
	claim      string
	moved      bool // the quote was elsewhere in the file than cited
}

// verifyFacts keeps the facts whose quote is in the cited file. A quote found
// near the cited lines, or anywhere else in the file, is kept with the lines
// it was really found at; a quote that is not in the file rejects the fact.
func verifyFacts(ws Workspace, facts []exploreFact) ([]verifiedFact, []string) {
	var out []verifiedFact
	var rejected []string
	seen := map[string]bool{}
	files := map[string][]byte{}
	for i, f := range facts {
		if i >= maxExploreFacts {
			rejected = append(rejected, fmt.Sprintf("%d more facts beyond the %d a report may carry", len(facts)-i, maxExploreFacts))
			break
		}
		path := strings.TrimSpace(f.Path)
		where := fmt.Sprintf("%s:%d", path, f.StartLine)
		quote := strings.TrimSpace(lineGutter.ReplaceAllString(f.Quote, ""))
		switch {
		case path == "":
			rejected = append(rejected, "a fact without a path")
			continue
		case quote == "":
			rejected = append(rejected, where+": no quote")
			continue
		case len([]rune(quote)) > maxQuoteRunes:
			rejected = append(rejected, where+": quote longer than three lines")
			continue
		}
		data, ok := files[path]
		if !ok {
			var err error
			data, err = readSourceFile(ws.ResolvePath(path))
			if err != nil {
				rejected = append(rejected, where+": "+err.Error())
				continue
			}
			files[path] = data
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		start, end, found := locateQuote(text, quote, int(f.StartLine), int(f.EndLine))
		if !found {
			rejected = append(rejected, where+": quote not in the file")
			continue
		}
		key := fmt.Sprintf("%s:%d-%d", path, start, end)
		if seen[key] {
			continue
		}
		seen[key] = true
		lines := strings.Split(text, "\n")
		out = append(out, verifiedFact{
			path: filepath.ToSlash(path), start: start, end: end,
			source: strings.Join(lines[start-1:end], "\n"),
			claim:  oneLine(f.Claim, maxClaimRunes),
			moved:  f.StartLine > 0 && int(f.StartLine) != start,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].path != out[j].path {
			return out[i].path < out[j].path
		}
		return out[i].start < out[j].start
	})
	return out, rejected
}

// locateQuote finds quote in text, first within a few lines of the cited
// range, then anywhere, and returns the 1-based lines the match spans.
func locateQuote(text, quote string, start, end int) (int, int, bool) {
	lines := strings.Split(text, "\n")
	if end < start {
		end = start
	}
	at := func(segment string, firstLine int) (int, int, bool) {
		loc := quoteIndex(segment, quote)
		if loc == nil || len([]rune(segment[loc[0]:loc[1]])) > maxQuoteRunes {
			return 0, 0, false
		}
		s := firstLine + strings.Count(segment[:loc[0]], "\n")
		return s, s + strings.Count(segment[loc[0]:loc[1]], "\n"), true
	}
	if start >= 1 && start <= len(lines) {
		from := max(1, start-3)
		to := min(len(lines), end+3)
		if s, e, ok := at(strings.Join(lines[from-1:to], "\n"), from); ok {
			return s, e, true
		}
	}
	return at(text, 1)
}

// readSourceFile reads a file the explorer cites, refusing directories,
// binaries and anything too large to be source.
func readSourceFile(abs string) ([]byte, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return nil, errors.New("no such file")
	}
	if info.IsDir() {
		return nil, errors.New("a directory, not a file")
	}
	if info.Size() > maxExploreFileSize {
		return nil, errors.New("file too large to check")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, errors.New("cannot be read")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxExploreFileSize))
	if err != nil {
		return nil, errors.New("cannot be read")
	}
	if strings.IndexByte(string(data[:min(len(data), 8192)]), 0) >= 0 {
		return nil, errors.New("a binary file")
	}
	return data, nil
}

// formatExplore renders what the deciding model receives. Quotes are the
// file's own text; the explorer's wording is labelled as such.
func formatExplore(question string, facts []verifiedFact, rejected []string, r *exploreReport, model string, rounds int, note string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "explore: %q\n", question)
	if note != "" {
		b.WriteString(note + "\n")
	}
	switch {
	case len(facts) == 0:
		b.WriteString("No fact could be checked against the files.")
	case len(facts) == 1:
		b.WriteString("1 fact, its quote checked against the file:")
	default:
		fmt.Fprintf(&b, "%d facts, every quote checked against the file:", len(facts))
	}
	if len(rejected) > 0 {
		fmt.Fprintf(&b, " (%d discarded: %s)", len(rejected), strings.Join(firstN(rejected, 4), "; "))
	}
	for _, f := range facts {
		loc := fmt.Sprintf("%s:%d", f.path, f.start)
		if f.end > f.start {
			loc += fmt.Sprintf("-%d", f.end)
		}
		if f.moved {
			loc += " (line corrected)"
		}
		b.WriteString("\n\n" + loc)
		src := strings.Split(f.source, "\n")
		for i, line := range src {
			if i == 3 {
				b.WriteString("\n  > …")
				break
			}
			b.WriteString("\n  > " + strings.TrimRight(line, " \t"))
		}
		if f.claim != "" {
			b.WriteString("\n  explorer's reading: " + f.claim)
		}
	}
	var missing []string
	for i, nf := range r.NotFound {
		if i >= maxExploreNotFound {
			break
		}
		what := oneLine(nf.What, 160)
		if what == "" {
			continue
		}
		if s := oneLine(nf.Searched, 160); s != "" {
			what += " — searched " + s
		}
		missing = append(missing, what)
	}
	if len(missing) > 0 {
		b.WriteString("\n\nnot found (not proof of absence):")
		for _, m := range missing {
			b.WriteString("\n- " + m)
		}
	}
	label := "explorer"
	if model != "" {
		label += " " + model
	}
	fmt.Fprintf(&b, "\n\n(%s, %d rounds. Quotes are the files' text; \"explorer's reading\" is its wording, not verified.)", label, rounds)
	return b.String()
}

func firstN(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return append(append([]string(nil), items[:n]...), fmt.Sprintf("%d more", len(items)-n))
}
