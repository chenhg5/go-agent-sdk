package agentsdk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConversationStore persists and retrieves conversation histories.
// Implement this interface for database-backed or remote storage.
type ConversationStore interface {
	Save(id string, messages []Message) error
	Load(id string) ([]Message, error)
	List() ([]string, error)
	Delete(id string) error
}

// ---------------------------------------------------------------------------
// FileStore — JSON-file-based implementation
// ---------------------------------------------------------------------------

// FileStore stores each conversation as a JSON file under Dir.
type FileStore struct {
	Dir string
}

var _ ConversationStore = (*FileStore)(nil)

// NewFileStore creates a FileStore, ensuring the directory exists.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	return &FileStore{Dir: dir}, nil
}

func (s *FileStore) path(id string) string {
	return filepath.Join(s.Dir, id+".json")
}

func (s *FileStore) Save(id string, messages []Message) error {
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}
	return os.WriteFile(s.path(id), data, 0644)
}

func (s *FileStore) Load(id string) ([]Message, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("unmarshal conversation: %w", err)
	}
	return msgs, nil
}

func (s *FileStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return ids, nil
}

func (s *FileStore) Delete(id string) error {
	return os.Remove(s.path(id))
}
