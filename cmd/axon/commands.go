package main

import (
	"strings"

	"github.com/atakang7/axon/agent"
)

// commands.go — slash-command dispatch.
//
// Slash commands are a CLI affordance, not a runtime concept. The
// runtime exposes the underlying operations (Reset, Undo, Cd) as
// methods on *agent.Agent; this file maps the `/<word>` strings the
// user types onto those methods.

func handleSlash(a *agent.Agent, s string) bool {
	switch {
	case strings.HasPrefix(s, "/cd "):
		target := strings.TrimSpace(strings.TrimPrefix(s, "/cd"))
		if cwd, err := a.Cd(target); err != nil {
			uiError(err)
		} else {
			uiInfo("cwd: " + cwd)
		}
		return true
	case s == "/pwd":
		uiInfo("cwd: " + a.Session().Cwd)
		return true
	case s == "/new":
		a.Reset()
		uiSessionNew()
		return true
	case s == "/undo":
		if path, ok := a.Undo(); ok {
			uiUndone(path)
		} else {
			uiInfo("nothing to undo")
		}
		return true
	case s == "/session":
		uiSessionInfo(a.Session())
		return true
	}
	return false
}
