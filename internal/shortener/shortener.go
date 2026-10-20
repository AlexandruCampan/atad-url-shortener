// Package shortener implements the URL shortener API with an optional durable JSON store.
package shortener

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"

type Link struct {
	ID        int        `json:"id"`
	Code      string     `json:"code"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	Visits    int        `json:"visits,omitempty"`
	LastVisit *time.Time `json:"last_visit_at,omitempty"`
}

type Store struct {
	mu       sync.Mutex
	links    map[string]Link
	nextID   int
	filePath string
}

type diskState struct {
	NextID int    `json:"next_id"`
	Links  []Link `json:"links"`
}

func New() *Store {
	return &Store{links: make(map[string]Link), nextID: 1}
}

// Open loads a durable store from path, or creates an empty store when the file
// does not exist yet. The parent directory is created as needed.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("data file path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	s := New()
	s.filePath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read data file: %w", err)
	}
	var saved diskState
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("decode data file: %w", err)
	}
	if saved.NextID < 1 {
		return nil, fmt.Errorf("data file has invalid next_id %d", saved.NextID)
	}
	seenIDs := make(map[int]struct{}, len(saved.Links))
	for _, link := range saved.Links {
		if !validCode(link.Code) || validateURL(link.URL) != nil || link.ID < 1 || link.Visits < 0 {
			return nil, fmt.Errorf("data file contains an invalid link")
		}
		if _, exists := s.links[link.Code]; exists {
			return nil, fmt.Errorf("data file contains duplicate code %q", link.Code)
		}
		if _, exists := seenIDs[link.ID]; exists || link.ID >= saved.NextID {
			return nil, fmt.Errorf("data file contains an invalid or duplicate link ID %d", link.ID)
		}
		seenIDs[link.ID] = struct{}{}
		s.links[link.Code] = link
	}
	s.nextID = saved.NextID
	return s, nil
}

func (s *Store) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /links", s.create)
	mux.HandleFunc("GET /links", s.list)
	mux.HandleFunc("GET /links/{code}/stats", s.stats)
	mux.HandleFunc("DELETE /links/{code}", s.delete)
	mux.HandleFunc("GET /{code}", s.redirect)
}

func (s *Store) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previousLinks, previousID := s.links, s.nextID
	s.links = make(map[string]Link)
	s.nextID = 1
	if err := s.persistLocked(); err != nil {
		s.links, s.nextID = previousLinks, previousID
		return err
	}
	return nil
}

func (s *Store) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URL  string `json:"url"`
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "request body must be valid JSON")
		return
	}
	if err := validateURL(input.URL); err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	if input.Code != "" && !validCode(input.Code) {
		writeError(w, http.StatusBadRequest, "validation", "code must be 4 to 10 letters, digits, hyphens or underscores")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	code := input.Code
	if code == "" {
		var err error
		for attempts := 0; attempts < 10; attempts++ {
			code, err = randomCode(7)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "could not generate a short code")
				return
			}
			if _, exists := s.links[code]; !exists {
				break
			}
			code = ""
		}
		if code == "" {
			writeError(w, http.StatusInternalServerError, "internal", "could not allocate a unique short code")
			return
		}
	} else if _, exists := s.links[code]; exists {
		writeError(w, http.StatusConflict, "conflict", "that short code is already in use")
		return
	}

	link := Link{ID: s.nextID, Code: code, URL: input.URL, CreatedAt: time.Now().UTC()}
	previousID := s.nextID
	s.nextID++
	s.links[code] = link
	if err := s.persistLocked(); err != nil {
		delete(s.links, code)
		s.nextID = previousID
		writeError(w, http.StatusInternalServerError, "internal", "could not save link")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(link)
}

func (s *Store) list(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	items := make([]Link, 0, len(s.links))
	for _, link := range s.links {
		items = append(items, link)
	}
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	writeJSON(w, http.StatusOK, items)
}

func (s *Store) stats(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	s.mu.Lock()
	link, ok := s.links[code]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "short code was not found")
		return
	}
	var lastVisit any
	if link.LastVisit != nil {
		lastVisit = link.LastVisit
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": link.Code, "visits": link.Visits, "last_visit_at": lastVisit,
	})
}

func (s *Store) delete(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	s.mu.Lock()
	_, ok := s.links[code]
	if ok {
		link := s.links[code]
		delete(s.links, code)
		if err := s.persistLocked(); err != nil {
			s.links[code] = link
			s.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "internal", "could not save deletion")
			return
		}
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "short code was not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Store) redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	s.mu.Lock()
	link, ok := s.links[code]
	if ok {
		previous := link
		now := time.Now().UTC()
		link.Visits++
		link.LastVisit = &now
		s.links[code] = link
		if err := s.persistLocked(); err != nil {
			s.links[code] = previous
			s.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "internal", "could not record visit")
			return
		}
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "short code was not found")
		return
	}
	http.Redirect(w, r, link.URL, http.StatusFound)
}

// persistLocked writes a complete snapshot with fsync and atomic rename. Callers
// hold s.mu so the snapshot and the mutation it represents stay in sync.
func (s *Store) persistLocked() error {
	if s.filePath == "" {
		return nil
	}
	items := make([]Link, 0, len(s.links))
	for _, link := range s.links {
		items = append(items, link)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	tmp, err := os.CreateTemp(filepath.Dir(s.filePath), ".links-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := json.NewEncoder(tmp).Encode(diskState{NextID: s.nextID, Links: items}); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.filePath); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(s.filePath)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func validateURL(raw string) error {
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("url must be an absolute http or https URL")
	}
	return nil
}

func validCode(code string) bool {
	if len(code) < 4 || len(code) > 10 {
		return false
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func randomCode(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	var code strings.Builder
	code.Grow(length)
	for _, b := range bytes {
		code.WriteByte(alphabet[int(b)%len(alphabet)])
	}
	return code.String(), nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
