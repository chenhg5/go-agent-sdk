package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// Session represents a single ACP conversation session backed by an SDK Agent.
type Session struct {
	ID    string
	CWD   string
	Agent agentsdk.Agent

	mu     sync.Mutex
	cancel context.CancelFunc // non-nil while a prompt is in progress
}

// SessionManager holds active sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

func (m *SessionManager) Create(cwd string, agent agentsdk.Agent) *Session {
	return m.CreateWithID(generateID(), cwd, agent)
}

func (m *SessionManager) CreateWithID(id, cwd string, agent agentsdk.Agent) *Session {
	s := &Session{
		ID:    id,
		CWD:   cwd,
		Agent: agent,
	}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s
}

func (m *SessionManager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	return s, ok
}

func (m *SessionManager) Delete(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// SetCancel stores the cancel function for the current prompt turn.
func (s *Session) SetCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
}

// Cancel cancels the current prompt turn, if any.
func (s *Session) Cancel() {
	s.mu.Lock()
	fn := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "sess_" + hex.EncodeToString(b)
}
