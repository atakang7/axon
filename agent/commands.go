package agent

import (
	"strings"
)

// commands.go — slash commands (/help, /reset, /undo, ...).

func (a *Agent) handleSlash(s string) bool {
	switch {
	case strings.HasPrefix(s, "/cd "):
		target := strings.TrimSpace(strings.TrimPrefix(s, "/cd"))
		if err := a.session.SetCwd(target); err != nil {
			uiError(err)
		} else {
			uiInfo("cwd: " + a.session.Cwd)
			a.session.Save()
		}
		return true
	case s == "/pwd":
		uiInfo("cwd: " + a.session.Cwd)
		return true
	case s == "/new":
		bgReg.killAll()
		a.session.Reset()
		a.initSessionMessages()
		tools, err := BuildTools(a.session, a.cfg)
		if err != nil {
			uiError(err)
			return true
		}
		a.tools = tools
		uiSessionNew()
		return true
	case s == "/undo":
		if e, ok := a.session.Undo(); ok {
			if err := writeBytesRaw(e.Path, []byte(e.Before)); err != nil {
				uiError(err)
			} else {
				uiUndone(e.Path)
				a.session.Save()
			}
		} else {
			uiInfo("nothing to undo")
		}
		return true
	case s == "/session":
		uiSessionInfo(a.session)
		return true
	}
	return false
}
