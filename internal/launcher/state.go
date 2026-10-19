package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the launcher's cache: which path was chosen and what has already been verified. Deleting
// it costs a re-check, never data. Nothing secret is stored here.
type State struct {
	Path    string               `json:"-"`
	Version string               `json:"version"`
	Kind    string               `json:"kind,omitempty"` // "solo" or "pilot"
	Steps   map[string]StepState `json:"steps"`
	Updated time.Time            `json:"updated"`
}

// StepState is one remembered check.
type StepState struct {
	Value string    `json:"value,omitempty"`
	OK    bool      `json:"ok"`
	At    time.Time `json:"at"`
}

// LoadState reads the state file and never fails: a missing or corrupt file is an empty state, so a
// half-written file cannot block a recovery.
func LoadState(path string) *State {
	st := &State{Path: path, Steps: map[string]StepState{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var onDisk State
	if err := json.Unmarshal(b, &onDisk); err != nil || onDisk.Steps == nil {
		return st
	}
	st.Version = onDisk.Version
	st.Kind = onDisk.Kind
	st.Steps = onDisk.Steps
	st.Updated = onDisk.Updated
	return st
}

// Mark records a check result.
func (s *State) Mark(id, value string, ok bool) {
	if s.Steps == nil {
		s.Steps = map[string]StepState{}
	}
	s.Steps[id] = StepState{Value: value, OK: ok, At: time.Now().UTC()}
	s.Updated = time.Now().UTC()
}

// Done reports whether a step was verified successfully at least once.
func (s *State) Done(id string) bool {
	step, ok := s.Steps[id]
	return ok && step.OK
}

// Save writes the file atomically with 0600.
func (s *State) Save() error {
	if s.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	s.Updated = time.Now().UTC()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}
