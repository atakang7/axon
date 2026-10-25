package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Goals are the conversation's contract: what each user message asked for,
// when it counts as done, and how that is proven. Status is computed from the
// log, never taken from the agent's word. See GOALS.md.

// Goal is keyed by the ID of the user message that opened it.
type Goal struct {
	// Continues names an earlier goal this message refines; criteria then
	// live on that goal.
	Continues   string       `json:"continues,omitempty"`
	Criteria    []*Criterion `json:"criteria,omitempty"`
	Constraints []string     `json:"constraints,omitempty"`
}

// Criterion is one checkable statement of done.
type Criterion struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"` // user | found | default | assumption
	// Oracle is the command whose output decides the criterion. Empty means
	// an observation, proven with an evidence block and quote.
	Oracle        string `json:"oracle,omitempty"`
	Expect        string `json:"expect,omitempty"`
	Baseline      string `json:"baseline,omitempty"`
	BaselineQuote string `json:"baseline_quote,omitempty"`
	// Since is the log length when the criterion was recorded, for the audit
	// trail; evidence validity depends on freshness (no write after it).
	Since        int       `json:"since"`
	Check        *Evidence `json:"check,omitempty"`
	Unverifiable string    `json:"unverifiable,omitempty"`
}

// Evidence cites a block and a quote that must appear in it.
type Evidence struct {
	ID    string `json:"id"`
	Quote string `json:"quote,omitempty"`
}

const (
	critOpen         = "open"
	critMet          = "met"
	critFailed       = "failed"
	critStale        = "stale"
	critUnverifiable = "unverifiable"
)

type critStatus struct {
	state    string
	evidence string
}

func (st critStatus) settled() bool { return st.state == critMet || st.state == critUnverifiable }

var exitCodeLine = regexp.MustCompile(`(?m)^exit_code: (-?\d+)`)

func execExitCode(content string) int {
	g := exitCodeLine.FindStringSubmatch(content)
	if g == nil {
		return -1
	}
	n, _ := strconv.Atoi(g[1])
	return n
}

func normalizeCommand(c string) string {
	return strings.Join(strings.Fields(c), " ")
}

// goalOf is the goal a user message's run works on, following continuations.
func (s *Session) goalOf(userID string) string {
	seen := map[string]bool{}
	for userID != "" && !seen[userID] {
		seen[userID] = true
		g := s.Goals[userID]
		if g == nil || g.Continues == "" {
			return userID
		}
		userID = g.Continues
	}
	return userID
}

// activeGoal is the goal of the open run, or "" before any user message.
func (s *Session) activeGoal() string {
	runs := sessionRuns(s)
	if len(runs) == 0 {
		return ""
	}
	return s.goalOf(s.Messages[runs[len(runs)-1].start].ID)
}

func (s *Session) ensureGoal(id string) *Goal {
	if s.Goals == nil {
		s.Goals = map[string]*Goal{}
	}
	if s.Goals[id] == nil {
		s.Goals[id] = &Goal{}
	}
	return s.Goals[id]
}

func (s *Session) messageIndex(id string) int {
	for i, m := range s.Messages {
		if m.ID == id {
			return i
		}
	}
	return -1
}

// lastWriteAfter is the index of the last successful write after index from,
// or -1.
func lastWriteAfter(s *Session, uses map[string]toolUse, from int) int {
	last := -1
	for i := max(from, 0); i < len(s.Messages); i++ {
		m := s.Messages[i]
		if m.Role == "tool" && !m.ToolFailed && uses[m.ToolCallID].name == toolWrite {
			last = i
		}
	}
	return last
}

// criterionStatus decides a criterion from the log alone.
func (s *Session) criterionStatus(c *Criterion, uses map[string]toolUse) critStatus {
	if c.Unverifiable != "" {
		return critStatus{state: critUnverifiable}
	}
	idx, id, state := -1, "", critOpen
	if c.Oracle != "" {
		oracle := normalizeCommand(c.Oracle)
		// Any run counts if no file changed after it: freshness, not the
		// recording time, is what makes evidence valid. A baseline taken
		// before the change goes stale at the change.
		for i := range s.Messages {
			m := s.Messages[i]
			if m.Role != "tool" || m.ToolFailed {
				continue
			}
			u := uses[m.ToolCallID]
			if u.name != toolExec || !strings.Contains(normalizeCommand(argString(u.args, "command")), oracle) {
				continue
			}
			idx, id = i, m.ID
			if execExitCode(m.Content) == 0 && (c.Expect == "" || strings.Contains(m.Content, c.Expect)) {
				state = critMet
			} else {
				state = critFailed
			}
		}
	} else if c.Check != nil {
		if idx = s.messageIndex(c.Check.ID); idx >= 0 {
			id, state = c.Check.ID, critMet
		}
	}
	if state == critMet && lastWriteAfter(s, uses, idx+1) > idx {
		state = critStale
	}
	return critStatus{state: state, evidence: id}
}

// goalWrites reports whether any run working on goal wrote a file, and the
// index of the last such write.
func (s *Session) goalWrites(goal string, uses map[string]toolUse) int {
	last := -1
	for _, r := range sessionRuns(s) {
		if s.goalOf(s.Messages[r.start].ID) != goal {
			continue
		}
		for i := r.start + 1; i < r.end; i++ {
			m := s.Messages[i]
			if m.Role == "tool" && !m.ToolFailed && uses[m.ToolCallID].name == toolWrite {
				last = i
			}
		}
	}
	return last
}

// defaultVerified is K0: a goal that changed files without recording criteria
// is verified by any successful command after its last write.
func (s *Session) defaultVerified(lastWrite int, uses map[string]toolUse) critStatus {
	for i := lastWrite + 1; i < len(s.Messages); i++ {
		m := s.Messages[i]
		if m.Role == "tool" && !m.ToolFailed && uses[m.ToolCallID].name == toolExec && execExitCode(m.Content) == 0 {
			return critStatus{state: critMet, evidence: m.ID}
		}
	}
	return critStatus{state: critOpen}
}

// openCriteria lists the active goal's criteria that are not settled, as
// "K1 failed" items. Changed files with no recorded criteria are open even
// when some command succeeded afterwards: replayed sessions showed users
// rejecting work in exactly those runs, because "a command passed" is not a
// check anyone chose. K0 is shown in the goal block for information only.
func (s *Session) openCriteria() []string {
	goal := s.activeGoal()
	if goal == "" {
		return nil
	}
	uses := toolUses(s)
	var out []string
	g := s.Goals[goal]
	if g == nil || len(g.Criteria) == 0 {
		if s.goalWrites(goal, uses) >= 0 {
			out = append(out, "no criteria recorded for the files you changed")
		}
		return out
	}
	for _, c := range g.Criteria {
		if st := s.criterionStatus(c, uses); !st.settled() {
			out = append(out, c.ID+" "+st.state)
		}
	}
	return out
}

// GoalBlock is sent at the end of every request: the active goal, its
// criteria with computed status, constraints, the plan, and other open goals.
func (s *Session) GoalBlock() string {
	task := s.TaskBlock()
	goal := s.activeGoal()
	if goal == "" {
		return task
	}
	uses := toolUses(s)
	var b strings.Builder
	b.WriteString("[goal]")
	runs := sessionRuns(s)
	opened := s.Messages[runs[len(runs)-1].start].ID
	fmt.Fprintf(&b, "\nactive: #%s %q", goal, truncateRunes(s.messageText(goal), 160))
	if opened != goal {
		fmt.Fprintf(&b, " (continued by #%s)", opened)
	}
	// A goal with nothing recorded and nothing changed has nothing to say;
	// questions and chat pay no tokens for the contract.
	say := false
	g := s.Goals[goal]
	if g != nil && len(g.Constraints) > 0 {
		say = true
		b.WriteString("\nconstraints (user's words): " + strings.Join(quoteAll(g.Constraints), "; "))
	}
	switch {
	case g != nil && len(g.Criteria) > 0:
		say = true
		b.WriteString("\ncriteria:")
		for _, c := range g.Criteria {
			b.WriteString("\n  " + s.criterionLine(c, s.criterionStatus(c, uses)))
		}
	default:
		if w := s.goalWrites(goal, uses); w >= 0 {
			say = true
			st := s.defaultVerified(w, uses)
			fmt.Fprintf(&b, "\ncriteria: none recorded for the files changed (record them with goal criteria). For information only, not a criterion: a successful command after the last write: %s%s", st.state, evidenceRef(st))
		}
	}
	if task != "" {
		b.WriteString("\n" + strings.Replace(task, "[task]", "plan:", 1))
	}
	var others []string
	for _, r := range runs[:len(runs)-1] {
		id := s.Messages[r.start].ID
		og := s.Goals[id]
		if id == goal || og == nil || og.Continues != "" || len(og.Criteria) == 0 {
			continue
		}
		var open []string
		for _, c := range og.Criteria {
			if st := s.criterionStatus(c, uses); !st.settled() {
				open = append(open, c.ID+" "+st.state)
			}
		}
		if len(open) > 0 {
			others = append(others, fmt.Sprintf("#%s %q (%s)", id, truncateRunes(s.messageText(id), 60), strings.Join(open, ", ")))
		}
	}
	if len(others) > 0 {
		say = true
		if len(others) > 3 {
			others = others[len(others)-3:]
		}
		b.WriteString("\nother open goals: " + strings.Join(others, "; "))
	}
	if !say {
		return task
	}
	if g != nil && len(g.Criteria) > 0 {
		b.WriteString("\nto finish: run each oracle after your last change; mark what you cannot check as unverifiable.")
	}
	return b.String()
}

func (s *Session) criterionLine(c *Criterion, st critStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s%s] %s", c.ID, st.state, evidenceRef(st), c.Text)
	if c.Source != "" {
		b.WriteString(" (" + c.Source + ")")
	}
	if c.Oracle != "" {
		fmt.Fprintf(&b, " | oracle `%s`", truncateRunes(c.Oracle, 160))
		if c.Expect != "" {
			fmt.Fprintf(&b, " expect %q", truncateRunes(c.Expect, 80))
		}
	}
	if c.Baseline != "" {
		fmt.Fprintf(&b, " | baseline #%s", c.Baseline)
		if c.BaselineQuote != "" {
			fmt.Fprintf(&b, " %q", truncateRunes(c.BaselineQuote, 120))
		}
	}
	if c.Unverifiable != "" {
		b.WriteString(" | cannot verify: " + truncateRunes(c.Unverifiable, 160))
	}
	return b.String()
}

func evidenceRef(st critStatus) string {
	if st.evidence == "" {
		return ""
	}
	return " #" + st.evidence
}

func quoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strconv.Quote(s)
	}
	return out
}

func (s *Session) messageText(id string) string {
	if i := s.messageIndex(id); i >= 0 {
		return s.Messages[i].Content
	}
	return ""
}

// goalTurnNote is the one-time note for an agent about to end the turn with
// the contract unsettled, or "" when there is nothing to say.
func (s *Session) goalTurnNote() string {
	open := s.openCriteria()
	if len(open) == 0 {
		return ""
	}
	return "[runtime note: you are about to finish, but the goal is not proven: " + strings.Join(open, ", ") +
		". If criteria are missing, record them now with goal criteria: the check the user would accept, with its oracle command. Then run each oracle after your last change. If one cannot be checked from your side, mark it with goal unverifiable and say so plainly in your answer. Do not describe unproven work as done. The user already saw your previous answer: do not repeat it, say only what changed.]"
}

// ---------------------------------------------------------------------------
// The goal tool
// ---------------------------------------------------------------------------

const goalDescription = `Record and check the contract of the current request: when it counts as done and how that is proven.
  - criteria: before changing anything, record checkable criteria. Each has an oracle (the exact command whose output decides it), expect (text that output must contain), and when you observed the current state first, baseline (block id) and baseline_quote (text from that block). constraints: the user's own words that must hold.
  - check: for a criterion without an oracle, cite the evidence block and a quote from it.
  - unverifiable: a criterion you cannot check from your side, with the reason.
  - continue: this message refines an earlier goal (its user message id); its criteria apply.
Status is computed from tool output: running an oracle IS the check, you do not report it.`

// goalCriterionInput is one criterion as the agent sends it.
type goalCriterionInput struct {
	Text          string `json:"text"`
	Oracle        string `json:"oracle"`
	Expect        string `json:"expect"`
	Baseline      string `json:"baseline"`
	BaselineQuote string `json:"baseline_quote"`
	Source        string `json:"source"`
}

// GoalTool records criteria and evidence. Every reference it is given is
// checked against the log before it is stored.
func GoalTool(s *Session) Tool {
	type input struct {
		Action      string               `json:"action"`
		Criteria    []goalCriterionInput `json:"criteria"`
		Constraints []string             `json:"constraints"`
		Criterion   string               `json:"criterion"`
		Evidence    string               `json:"evidence"`
		Quote       string               `json:"quote"`
		Reason      string               `json:"reason"`
		Goal        string               `json:"goal"`
	}
	critSchema := obj("object", props{
		"text":           strSchema("The checkable statement."),
		"oracle":         strSchema("Exact command whose output decides it. Omit for an observation."),
		"expect":         strSchema("Text the oracle output must contain."),
		"baseline":       strSchema("Block id where you observed the current state."),
		"baseline_quote": strSchema("Text from the baseline block."),
		"source":         enumSchema("Where the criterion comes from.", "user", "found", "default", "assumption"),
	}, []string{"text"})
	return Tool{
		Name:        toolGoal,
		Description: goalDescription,
		Schema: obj("object", props{
			"action":      enumSchema("criteria | check | unverifiable | continue.", "criteria", "check", "unverifiable", "continue"),
			"criteria":    arr(critSchema),
			"constraints": arr(strSchema("The user's own words, verbatim.")),
			"criterion":   strSchema("Criterion id, e.g. K2."),
			"evidence":    strSchema("Block id of the evidence."),
			"quote":       strSchema("Text from the evidence block."),
			"reason":      strSchema("Why it cannot be checked."),
			"goal":        strSchema("For continue: the earlier goal's user message id."),
		}, []string{"action"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			goal := s.activeGoal()
			if goal == "" {
				return "", fmt.Errorf("no user request to attach a goal to")
			}
			switch p.Action {
			case "continue":
				return s.continueGoal(strings.TrimPrefix(p.Goal, "#"))
			case "criteria":
				return s.recordCriteria(goal, p.Criteria, p.Constraints)
			case "check", "unverifiable":
				return s.settleCriterion(goal, p.Action, strings.ToUpper(strings.TrimSpace(p.Criterion)), strings.TrimPrefix(p.Evidence, "#"), p.Quote, p.Reason)
			}
			return "", fmt.Errorf("action is required: criteria | check | unverifiable | continue")
		},
	}
}

func (s *Session) continueGoal(target string) (string, error) {
	runs := sessionRuns(s)
	opened := s.Messages[runs[len(runs)-1].start].ID
	if target == "" || target == opened {
		return "", fmt.Errorf("goal: give the user message id of the earlier goal")
	}
	i := s.messageIndex(target)
	if i < 0 || s.Messages[i].Role != "user" {
		return "", fmt.Errorf("goal: %s is not a user message", target)
	}
	if s.goalOf(target) == opened {
		return "", fmt.Errorf("goal: %s already continues this request", target)
	}
	s.ensureGoal(opened).Continues = target
	return fmt.Sprintf("this request continues goal #%s; its criteria apply", s.goalOf(opened)), s.Save()
}

// recordCriteria stores what it can verify and reports what it rejected, so
// one bad reference does not lose the rest.
func (s *Session) recordCriteria(goal string, in []goalCriterionInput, constraints []string) (string, error) {
	g := s.ensureGoal(goal)
	var added, rejected []string
	for _, c := range in {
		c.Text = strings.TrimSpace(c.Text)
		if c.Text == "" {
			rejected = append(rejected, "a criterion without text")
			continue
		}
		cr := &Criterion{Text: c.Text, Oracle: strings.TrimSpace(c.Oracle), Expect: c.Expect, Source: c.Source,
			Baseline: strings.TrimPrefix(strings.TrimSpace(c.Baseline), "#"), Since: len(s.Messages)}
		if cr.Baseline != "" {
			i := s.messageIndex(cr.Baseline)
			if i < 0 {
				rejected = append(rejected, fmt.Sprintf("%q: baseline block %s does not exist", c.Text, cr.Baseline))
				continue
			}
			if q := strings.TrimSpace(c.BaselineQuote); q != "" {
				quote, ok := groundBlockQuote(s.Messages[i].Content, q)
				if !ok {
					rejected = append(rejected, fmt.Sprintf("%q: baseline quote is not in %s", c.Text, cr.Baseline))
					continue
				}
				cr.BaselineQuote = quote
			}
		}
		cr.ID = fmt.Sprintf("K%d", len(g.Criteria)+1)
		g.Criteria = append(g.Criteria, cr)
		added = append(added, cr.ID+" "+truncateRunes(cr.Text, 60))
	}
	for _, c := range constraints {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !s.userSaid(c) {
			rejected = append(rejected, fmt.Sprintf("constraint %q: not the user's words", c))
			continue
		}
		g.Constraints = append(g.Constraints, c)
		added = append(added, "constraint "+strconv.Quote(truncateRunes(c, 60)))
	}
	if len(added) == 0 {
		return "", fmt.Errorf("goal: nothing recorded: %s", strings.Join(rejected, "; "))
	}
	msg := "recorded: " + strings.Join(added, "; ")
	if len(rejected) > 0 {
		msg += "\nrejected: " + strings.Join(rejected, "; ")
	}
	return msg, s.Save()
}

func (s *Session) userSaid(text string) bool {
	for _, m := range s.Messages {
		if m.Role == "user" {
			if _, ok := groundQuote(m.Content, text); ok {
				return true
			}
		}
	}
	return false
}

func (s *Session) settleCriterion(goal, action, id, evidence, quote, reason string) (string, error) {
	g := s.Goals[goal]
	var c *Criterion
	if g != nil {
		for _, cr := range g.Criteria {
			if cr.ID == id {
				c = cr
			}
		}
	}
	if c == nil {
		return "", fmt.Errorf("goal: no criterion %q on the active goal", id)
	}
	if action == "unverifiable" {
		if strings.TrimSpace(reason) == "" {
			return "", fmt.Errorf("goal: say why %s cannot be checked", id)
		}
		c.Unverifiable = strings.TrimSpace(reason)
		return fmt.Sprintf("%s marked unverifiable; say so in your answer", id), s.Save()
	}
	if c.Oracle != "" {
		return "", fmt.Errorf("goal: %s has an oracle; run `%s` and its status is computed from the output", id, c.Oracle)
	}
	i := s.messageIndex(evidence)
	if i < 0 || s.Messages[i].Role != "tool" || s.Messages[i].ToolFailed {
		return "", fmt.Errorf("goal: evidence must be a successful tool result block")
	}
	q, ok := groundBlockQuote(s.Messages[i].Content, strings.TrimSpace(quote))
	if !ok || strings.TrimSpace(quote) == "" {
		return "", fmt.Errorf("goal: the quote is not in %s", evidence)
	}
	c.Check = &Evidence{ID: evidence, Quote: q}
	return fmt.Sprintf("%s met by #%s", id, evidence), s.Save()
}

// hasBrokenCriterion is true when an open item is failed, stale or missing,
// as opposed to merely not run yet.
func hasBrokenCriterion(open []string) bool {
	for _, o := range open {
		if strings.HasSuffix(o, " "+critFailed) || strings.HasSuffix(o, " "+critStale) || strings.HasPrefix(o, "no criteria") {
			return true
		}
	}
	return false
}

// brokenGoalNote is the second and last note of a turn: the agent ended again
// with the contract broken, so its answer must say so.
func brokenGoalNote(open []string) string {
	return "[runtime note: the goal is still not proven: " + strings.Join(open, ", ") +
		". Do not say the work is done. Either fix it and run the oracles, or answer plainly which criteria failed or are unverified, citing the evidence block ids. Do not repeat your previous answer.]"
}

var lineGutter = regexp.MustCompile(`(?m)^\s*\d+\t`)

// groundBlockQuote finds quote in a tool result, also when the agent quoted
// several lines of a read without the line-number gutter the read added.
func groundBlockQuote(content, quote string) (string, bool) {
	if q, ok := groundQuote(content, quote); ok {
		return q, true
	}
	return groundQuote(lineGutter.ReplaceAllString(content, ""), quote)
}

// GoalView is the active goal as an editor shows it.
type GoalView struct {
	Goal     string          `json:"goal"`
	Intent   string          `json:"intent"`
	Criteria []CriterionView `json:"criteria"`
	// Unchecked is set when files changed and no criteria were recorded.
	Unchecked bool `json:"unchecked,omitempty"`
}

// CriterionView is one criterion with its computed status.
type CriterionView struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	State    string `json:"state"`
	Evidence string `json:"evidence,omitempty"`
	Oracle   string `json:"oracle,omitempty"`
	Note     string `json:"note,omitempty"`
}

// ActiveGoalView reports the goal of the open run, or nil before any user
// message or when the goal has nothing recorded and nothing changed.
func (s *Session) ActiveGoalView() *GoalView {
	goal := s.activeGoal()
	if goal == "" {
		return nil
	}
	uses := toolUses(s)
	v := &GoalView{Goal: goal, Intent: truncateRunes(s.messageText(goal), 240)}
	if g := s.Goals[goal]; g != nil {
		for _, c := range g.Criteria {
			st := s.criterionStatus(c, uses)
			v.Criteria = append(v.Criteria, CriterionView{ID: c.ID, Text: c.Text, State: st.state, Evidence: st.evidence, Oracle: c.Oracle, Note: c.Unverifiable})
		}
	}
	if len(v.Criteria) == 0 {
		if s.goalWrites(goal, uses) < 0 {
			return nil
		}
		v.Unchecked = true
	}
	return v
}
