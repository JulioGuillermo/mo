package plugins

import (
	lua "github.com/yuin/gopher-lua"
)

const maxSendBytes = 64 << 10

// SentMessage is what a plugin wants the model to read. Wake starts a
// turn for it when the model is idle; without it the model reads it with
// the next message.
type SentMessage struct {
	Plugin, Title, Text string
	Wake                bool
}

// OnSend sets who takes what plugins send to the model, to decide whether
// it gets in.
func (s *Set) OnSend(take func(SentMessage)) {
	if s == nil {
		return
	}
	s.surface.mu.Lock()
	defer s.surface.mu.Unlock()
	s.surface.send = take
}

// sendToModel is mo.send{ title, message, wake }: it hands the message to
// mo, which lets it in, asks the user or judges it as the permission mode
// says. It gives whether there was a conversation to send it to.
func (p *Plugin) sendToModel(state *lua.LState) int {
	spec := state.CheckTable(1)
	title := luaString(spec.RawGetString("title"))
	text := luaString(spec.RawGetString("message"))
	if title == "" || text == "" {
		state.RaiseError("mo.send needs a title and a message")
	}
	if len(text) > maxSendBytes {
		state.RaiseError("mo.send: the message is over %d KB", maxSendBytes>>10)
	}
	wake := spec.RawGetString("wake") != lua.LFalse
	p.surface.mu.Lock()
	take := p.surface.send
	p.surface.mu.Unlock()
	if take == nil {
		p.log("mo.send: no conversation to send", title, "to")
		state.Push(lua.LFalse)
		return 1
	}
	go take(SentMessage{Plugin: p.name, Title: title, Text: text, Wake: wake})
	state.Push(lua.LTrue)
	return 1
}
