package wiki

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Store interface {
	QueryContext(roomID, goal string) string
}

type fileStore struct {
	baseDir string
}

func NewFileStore(baseDir string) Store {
	return &fileStore{baseDir: ResolveBaseDir(baseDir)}
}

func ResolveBaseDir(baseDir string) string {
	if strings.TrimSpace(baseDir) == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "lzy", "wiki")
	}
	return strings.TrimSpace(baseDir)
}

func (s *fileStore) roomDir(roomID string) string {
	return filepath.Join(s.baseDir, "room-"+strings.TrimSpace(roomID))
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s *fileStore) QueryContext(roomID, _ string) string {
	if roomID == "" {
		return ""
	}
	return loadWikiContext(s.roomDir(roomID), "群聊", roomID)
}

func loadWikiContext(dir, label, id string) string {
	index := readFile(filepath.Join(dir, "index.md"))
	if index == "" {
		log.Printf("[wiki] QueryContext %s=%s: no index found", label, id)
		return ""
	}
	log.Printf("[wiki] QueryContext %s=%s: loaded index %d chars", label, id, len(index))
	return "## " + label + "知识库\n" + index
}
