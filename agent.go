package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const codingAgentSystemPrompt = `You are a coding agent. Tools: list_files, read_file, edit_file. When asked to investigate or change code: call tools to gather what you need, then respond. Do not narrate before acting. Do not repeat a tool call you already made in this conversation.`

type Agent struct {
	client         ChatClient
	getUserMessage func() (string, bool)
	tools          []ToolDefinition
	session        *Session
	ui             UI
}

func NewAgent(client ChatClient, getUserMessage func() (string, bool), tools []ToolDefinition, session *Session) *Agent {
	return &Agent{client: client, getUserMessage: getUserMessage, tools: tools, session: session, ui: UI{}}
}

func (a *Agent) Run(ctx context.Context) error {
	if len(a.session.Messages) == 0 {
		a.session.Messages = []ChatMessage{{Role: "system", Content: codingAgentSystemPrompt}}
	}
	readUserInput := true
	for {
		if readUserInput {
			a.ui.Prompt()
			userInput, ok := a.getUserMessage()
			if !ok {
				break
			}
			a.ui.AfterInput()

			switch strings.TrimSpace(userInput) {
			case "/new":
				a.session = NewSession()
				a.session.Messages = []ChatMessage{{Role: "system", Content: codingAgentSystemPrompt}}
				a.ui.SessionNew(a.session.ID)
				continue
			case "/undo":
				if edit, ok := a.session.Undo(); ok {
					if err := writeFile(edit.Path, edit.Before); err != nil {
						a.ui.Error(err)
					} else {
						a.ui.Undone(edit.Path)
						a.session.Save()
					}
				} else {
					a.ui.Info("nothing to undo")
				}
				continue
			case "/session":
				a.ui.SessionInfo(a.session)
				continue
			}

			a.session.Messages = append(a.session.Messages, ChatMessage{Role: "user", Content: userInput})
			a.session.Save()
		}

		message, err := a.chat(ctx)
		if err != nil {
			a.ui.Error(err)
			readUserInput = true
			continue
		}
		a.session.Messages = append(a.session.Messages, *message)
		a.session.Save()

		if len(message.ToolCalls) == 0 {
			readUserInput = true
			continue
		}

		for _, tc := range message.ToolCalls {
			result := a.executeTool(tc.ID, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			a.session.Messages = append(a.session.Messages, result)
			a.session.Save()
		}
		readUserInput = false
	}
	return nil
}

func (a *Agent) chat(ctx context.Context) (*ChatMessage, error) {
	var err error
	var msg *ChatMessage
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		tokens := make(chan string, 4096)
		done := make(chan struct{})
		go func() {
			defer close(done)
			started := false
			for t := range tokens {
				if !started {
					started = true
					a.ui.StartResponse()
				}
				a.ui.Token(t)
			}
			if started {
				a.ui.Response()
			}
		}()
		stop := a.ui.Spinner()
		first := true
		msg, err = a.client.Chat(ctx, a.session.Messages, a.tools, func(token string) {
			if first {
				first = false
				stop()
			}
			tokens <- token
		})
		stop()
		close(tokens)
		<-done
		if err == nil {
			return msg, nil
		}
		a.ui.Error(err)
	}
	return nil, err
}

func (a *Agent) executeTool(id, name string, input json.RawMessage) ChatMessage {
	for _, tool := range a.tools {
		if tool.Name == name {
			a.ui.Tool(name, input)
			response, err := tool.Function(input)
			if err != nil {
				a.ui.ToolError(err)
				return ChatMessage{Role: "tool", ToolCallID: id, Content: err.Error()}
			}
			a.ui.ToolResult(response)
			return ChatMessage{Role: "tool", ToolCallID: id, Content: response}
		}
	}
	return ChatMessage{Role: "tool", ToolCallID: id, Content: "tool not found"}
}

func writeFile(path, content string) error {
	return writeFileBytes(path, []byte(content))
}
