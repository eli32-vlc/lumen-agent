package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Store struct {
	path string
	data map[string]string
}

func Load(path string) (*Store, error) {
	if path == "" {
		return &Store{path: path, data: make(map[string]string)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{path: path, data: make(map[string]string)}, nil
		}
		return nil, fmt.Errorf("read secrets file: %w", err)
	}

	var secrets map[string]string
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("decode secrets file: %w", err)
	}

	return &Store{path: path, data: secrets}, nil
}

func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write secrets file: %w", err)
	}
	return nil
}

func (s *Store) Add(name, value string) error {
	if s.data == nil {
		s.data = make(map[string]string)
	}
	s.data[name] = value
	return s.Save()
}

func (s *Store) Delete(name string) error {
	if s.data == nil {
		return nil
	}
	delete(s.data, name)
	return s.Save()
}

func (s *Store) Names() []string {
	if s == nil || len(s.data) == 0 {
		return nil
	}

	names := make([]string, 0, len(s.data))
	for name := range s.data {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Store) Substitute(text string) string {
	if s == nil || len(s.data) == 0 {
		return text
	}

	result := text
	for name, value := range s.data {
		placeholder := fmt.Sprintf("{{secret:%s}}", name)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

func (s *Store) Redact(text string) string {
	if s == nil || len(s.data) == 0 {
		return text
	}

	result := text
	// Sort by value length descending to avoid partial redaction if one secret is a substring of another
	type pair struct {
		name  string
		value string
	}
	pairs := make([]pair, 0, len(s.data))
	for name, value := range s.data {
		if value == "" {
			continue
		}
		pairs = append(pairs, pair{name: name, value: value})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return len(pairs[i].value) > len(pairs[j].value)
	})

	for _, p := range pairs {
		redacted := fmt.Sprintf("[REDACTED:%s]", p.name)
		result = strings.ReplaceAll(result, p.value, redacted)
	}
	return result
}
