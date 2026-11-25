package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// Session reuse.
//
// agy reads at most acpPromptBudget bytes of one prompt, while a Claude Code conversation grows far past that; an ACP session, though, keeps everything it was sent, up to agy's context window of about a million tokens. So after an answer the session stays open, filed under the conversation it now holds, and the next request of that conversation (the same messages, the answer as Claude Code echoes it, and one new user message) sends only the new message. Everything else, such as a new subagent or the first request after the daemon or agy restarted, gets a new session that reads the whole conversation, in several prompts when it is long. Losing the idle sessions therefore costs time, never correctness.
//
// agy cannot fork a session, yet at the end of every turn Claude Code sends background requests (a prompt suggestion, memory extraction, a summary) that carry the main thread's whole history plus a request of their own. Claude Code's hint headers (translate.Origin) say who sent each request, and sessions belong to their sender:
//   - The main thread continues only its own session, so background requests never take it and the next message never reads the conversation again.
//   - A compaction continues the main thread's session, so its summary reads the whole conversation at the cost of one instruction; the session is closed afterwards, as the main thread starts over from the summary.
//   - Every other sender (a subagent, a workflow agent, one kind of background request) keeps a session of its own. A background session also remembers the point of the main history it branched from, so its next request, a turn later, sends only what the main thread did since, with a note to disregard the earlier background exchange.
//
// Without the headers (an older Claude Code), any request continues a session holding its conversation, and a background request may take the main thread's session, which then costs the next message a new session.

const (
	// acpIdleSessions is how many answered sessions one agy keeps for reuse. Sessions of a main thread are closed last.
	acpIdleSessions = 16
	// acpIdleTimeout is how long an answered session waits for the next request of its conversation.
	acpIdleTimeout = 30 * time.Minute
)

// acpRole is how a request may use the sessions of others.
type acpRole int

const (
	// roleUnknown: Claude Code did not say who sent the request.
	roleUnknown acpRole = iota
	// roleMain: the main thread of a Claude Code session.
	roleMain
	// roleCompaction: a compaction of the main thread, which ends that thread's session.
	roleCompaction
	// roleOther: a subagent, a workflow agent or a background request.
	roleOther
)

// originRole returns the role of a request and the owner of the sessions it uses: the main thread, one agent, or one kind of background request, of one Claude Code session.
func originRole(o translate.Origin) (acpRole, string) {
	switch {
	case o.Class == "":
		return roleUnknown, ""
	case o.Class == "compaction":
		return roleCompaction, o.Session + "\x00main"
	case o.Agent != "":
		return roleOther, o.Session + "\x00agent\x00" + o.Agent
	case o.Class == "main":
		return roleMain, o.Session + "\x00main"
	}
	return roleOther, o.Session + "\x00" + o.Class + "\x00" + o.Kind
}

// acpSession is one ACP session and what NeoSwitch has told it.
type acpSession struct {
	id      acp.SessionId
	options []acp.SessionConfigOption
	// system and tools are the hashes of the system instructions and tool definitions the session read last.
	system, tools string
	// level is the thought level NeoSwitch selected, or "" for agy's default.
	level acp.SessionConfigValueId
	// model is the model that answers the session; a request continues only a session of the model it asks for.
	model string
	// owner is the owner of the request the session answered last (originRole), "" when Claude Code did not say.
	owner string
	main  bool
	// key is the conversation the session holds, its last answer included (historyKey).
	key string
	// branch is, for a background session, the main history it continued last, which its next request extends.
	branch    string
	idleSince time.Time

	// toolCalls are the tool calls agy reported for the session, which the tool guard must have decided on.
	toolMu    sync.Mutex
	toolCalls map[acp.ToolCallId]bool
}

func (s *acpSession) sawToolCall(id acp.ToolCallId) {
	s.toolMu.Lock()
	defer s.toolMu.Unlock()
	if s.toolCalls == nil {
		s.toolCalls = map[acp.ToolCallId]bool{}
	}
	s.toolCalls[id] = true
}

func (s *acpSession) toolCallCount() int {
	s.toolMu.Lock()
	defer s.toolMu.Unlock()
	return len(s.toolCalls)
}

// historyKeys identifies every prefix of a conversation: keys[m] is the key of contents[:m]. A key covers what reaches the model: the text of each message, its attachments, its tool calls by ID and name, and its tool results with their IDs. Thought signatures, Claude Code's cache markers and the split of a text into blocks do not matter, so a model turn hashes the same whether NeoSwitch built it from agy's answer or translated it from the copy Claude Code sends back.
func historyKeys(contents []*genai.Content) []string {
	h := sha256.New()
	keys := make([]string, 0, len(contents)+1)
	keys = append(keys, hex.EncodeToString(h.Sum(nil)))
	for _, content := range contents {
		if content != nil {
			writeContent(h, content)
		}
		keys = append(keys, hex.EncodeToString(h.Sum(nil)))
	}
	return keys
}

// historyKey is the key of the whole conversation.
func historyKey(contents []*genai.Content) string {
	keys := historyKeys(contents)
	return keys[len(keys)-1]
}

func writeContent(h hash.Hash, content *genai.Content) {
	writeField(h, "role", string(content.Role))
	var text strings.Builder
	flush := func() {
		if t := strings.TrimSpace(text.String()); t != "" {
			writeField(h, "text", t)
		}
		text.Reset()
	}
	for _, part := range content.Parts {
		if part == nil || part.Thought {
			continue
		}
		text.WriteString(part.Text)
		if part.InlineData == nil && part.FunctionCall == nil && part.FunctionResponse == nil {
			continue
		}
		flush()
		if part.InlineData != nil {
			writeBlob(h, part.InlineData.MIMEType, part.InlineData.Data)
		}
		if call := part.FunctionCall; call != nil {
			// A call is identified by its ID, which is unique, and not by its input: Claude Code sends back the input as its tool read it, not as the model wrote it. Its Edit tool, for one, adds "replace_all": false, so keying on the input loses the session after every edit.
			args := ""
			if call.ID == "" {
				args = canonicalJSON(call.Args)
			}
			writeField(h, "call", call.ID+"\x00"+call.Name+"\x00"+args)
		}
		if response := part.FunctionResponse; response != nil {
			writeField(h, "result", response.ID+"\x00"+response.Name+"\x00"+canonicalJSON(response.Response))
			for _, media := range response.Parts {
				if media != nil && media.InlineData != nil {
					writeBlob(h, media.InlineData.MIMEType, media.InlineData.Data)
				}
			}
		}
	}
	flush()
	writeField(h, "end", "")
}

// writeField writes a length-prefixed field, so that no two different conversations write the same bytes.
func writeField(h hash.Hash, name, value string) {
	h.Write([]byte(name + ":" + strconv.Itoa(len(value)) + ":" + value))
}

func writeBlob(h hash.Hash, mimeType string, data []byte) {
	sum := sha256.Sum256(data)
	writeField(h, "data", mimeType+"\x00"+hex.EncodeToString(sum[:]))
}

// canonicalJSON encodes a decoded JSON object with sorted keys; an absent object and an empty one are the same.
func canonicalJSON(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func textKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// claim removes and returns the idle session a request may continue, with how many of the request's messages it holds already; nil when there is none. keys are the request's historyKeys. Sessions idle for longer than acpIdleTimeout are closed on the way.
func (c *acpConn) claim(keys []string, role acpRole, owner, model string) (*acpSession, int) {
	c.mu.Lock()
	expired := c.expireLocked(time.Now())
	defer func() { go c.closeSessions(expired) }()
	defer c.mu.Unlock()
	held := len(keys) - 2 // every message but the new one
	for i, s := range c.idle {
		if s.key != keys[held] || s.model != model {
			continue
		}
		mayTake := false
		switch role {
		case roleUnknown:
			mayTake = true
		case roleMain, roleCompaction:
			mayTake = s.owner == owner || s.owner == ""
		case roleOther:
			mayTake = s.owner == owner
		}
		if mayTake {
			c.idle = slices.Delete(c.idle, i, i+1)
			return s, held
		}
	}
	if role != roleOther {
		return nil, 0
	}
	// A background session continues from the main history it branched from, when this request extends it; the longest such history wins.
	at := make(map[string]int, len(keys))
	for m, key := range keys[:held+1] {
		at[key] = m
	}
	best, from := -1, 0
	for i, s := range c.idle {
		if m, ok := at[s.branch]; ok && s.owner == owner && s.model == model && s.branch != "" && m > from {
			best, from = i, m
		}
	}
	if best < 0 {
		return nil, 0
	}
	s := c.idle[best]
	c.idle = slices.Delete(c.idle, best, best+1)
	return s, from
}

// keepSession files an answered session for the next request of its conversation. A connection that is retired or has exited keeps nothing. Over acpIdleSessions, the session answered longest ago is closed, those of main threads last.
func (c *acpConn) keepSession(s *acpSession) {
	now := time.Now()
	c.mu.Lock()
	select {
	case <-c.conn.Done():
		c.mu.Unlock()
		return
	default:
	}
	if c.retired {
		c.mu.Unlock()
		c.closeSessions([]*acpSession{s})
		return
	}
	s.idleSince = now
	c.idle = append(c.idle, s)
	closing := c.expireLocked(now)
	for len(c.idle) > acpIdleSessions {
		evict := evictionChoice(c.idle)
		closing = append(closing, c.idle[evict])
		c.idle = slices.Delete(c.idle, evict, evict+1)
	}
	c.mu.Unlock()
	go c.closeSessions(closing)
}

// evictionChoice picks the idle session to close first: the one answered longest ago, among those of main threads only when no other is left. A main thread's session is the one a person waits on, while background sessions are answered after it and would otherwise push it out.
func evictionChoice(idle []*acpSession) int {
	choice := 0
	for i, s := range idle {
		if s.main != idle[choice].main {
			if !s.main {
				choice = i
			}
			continue
		}
		if s.idleSince.Before(idle[choice].idleSince) {
			choice = i
		}
	}
	return choice
}

func (c *acpConn) expireLocked(now time.Time) []*acpSession {
	var expired []*acpSession
	c.idle = slices.DeleteFunc(c.idle, func(s *acpSession) bool {
		if now.Sub(s.idleSince) > acpIdleTimeout {
			expired = append(expired, s)
			return true
		}
		return false
	})
	return expired
}

// closeSessions ends sessions in agy. It uses a context of its own, so that a cancelled request still releases its session.
func (c *acpConn) closeSessions(sessions []*acpSession) {
	for _, s := range sessions {
		ctx, cancel := context.WithTimeout(context.Background(), acpCloseTimeout)
		_, _ = c.conn.CloseSession(ctx, acp.CloseSessionRequest{SessionId: s.id})
		cancel()
		forgetDecisions(c.workspace, string(s.id))
	}
}

// acpExchange is a request's hold on its session and on the agy serving it, from the answer until NeoSwitch knows whether Claude Code got that answer.
type acpExchange struct {
	c       *acpConn
	session *acpSession
	// contents is the conversation the session holds, without the answer.
	contents []*genai.Content
	role     acpRole
	owner    string
	// model is the model the request asked for (modelFor).
	model string
	// branch is the main history a background session continues from, when it started from it or caught up with it for this request; "" when it continued its own exchange.
	branch string
	result acp.PromptResponse
}

// end releases the connection. The session is kept for the conversation's next request only when answer is exactly the model turn Claude Code received and will send back; otherwise it is closed, since the next request could not match it, or would build on an answer Claude Code never saw. A compaction's session is closed too: the main thread continues from the summary.
func (x *acpExchange) end(answer []*genai.Part, delivered bool) {
	defer x.c.release()
	if !delivered || len(answer) == 0 || x.result.StopReason != acp.StopReasonEndTurn || x.role == roleCompaction {
		x.c.closeSessions([]*acpSession{x.session})
		return
	}
	s := x.session
	s.owner, s.main = x.owner, x.role == roleMain
	s.key = historyKey(append(slices.Clone(x.contents), &genai.Content{Role: genai.RoleModel, Parts: answer}))
	if x.branch != "" {
		s.branch = x.branch
	}
	x.c.keepSession(s)
}
