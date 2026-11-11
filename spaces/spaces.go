package spaces

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/johanhenriksson/remux/backend"
	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/registry"
)

// Space represents a loaded workspace with config.
type Space struct {
	Name     string
	Branch   string
	Path     string
	Port     int
	RepoRoot string
	// SetupPending is true until the first session has been created and the
	// on_create hooks have run.
	SetupPending bool
	config       *config.Config
}

// ID returns a sanitized identifier for the space (slashes and hyphens replaced with underscores).
func (s *Space) ID() string {
	id := strings.ReplaceAll(s.Name, "/", "_")
	return strings.ReplaceAll(id, "-", "_")
}

// Open loads a space from the given worktree path.
// It loads both the registry entry and workspace config.
func Open(worktreePath string) (*Space, error) {
	destDir := filepath.Dir(worktreePath)
	spaceName := filepath.Base(worktreePath)

	reg, err := registry.Load(destDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	entry := reg.Get(spaceName)
	if entry == nil {
		return nil, fmt.Errorf("space not found: %s", spaceName)
	}

	cfg, err := config.Load(worktreePath, entry.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	space := &Space{
		Name:         entry.Name,
		Branch:       entry.Branch,
		Path:         entry.Path,
		Port:         entry.Port,
		RepoRoot:     entry.RepoRoot,
		SetupPending: entry.SetupPending,
		config:       cfg,
	}

	return space, nil
}

// configSpace returns the config.Space context for template evaluation.
func (s *Space) configSpace() config.Space {
	return config.NewSpace(s.Name, s.Path, s.Port, s.RepoRoot)
}

// RunOnCreate executes on_create hooks. Prints warnings on failure.
func (s *Space) RunOnCreate() {
	s.config.RunOnCreate(s.configSpace())
}

// RunOnOpen executes on_open hooks. Returns error on failure.
func (s *Space) RunOnOpen() error {
	return s.config.RunOnOpen(s.configSpace())
}

// RunOnDrop executes on_drop hooks. Returns error on failure.
func (s *Space) RunOnDrop() error {
	return s.config.RunOnDrop(s.configSpace())
}

// ResolveEnv evaluates template expressions in config env vars.
func (s *Space) ResolveEnv() (map[string]string, error) {
	return s.config.ResolveEnv(s.configSpace())
}

// SessionEnv returns the env every shell in the space's session should see:
// the resolved config env plus SPACE_PORT.
func (s *Space) SessionEnv() (map[string]string, error) {
	env, err := s.ResolveEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config env vars: %w", err)
	}
	if env == nil {
		env = make(map[string]string)
	}
	// todo: maybe no longer required?
	if _, ok := env["SPACE_PORT"]; !ok {
		env["SPACE_PORT"] = strconv.Itoa(s.Port)
	}
	return env, nil
}

// Agent returns the configured agent for this space, if any.
func (s *Space) Agent() string {
	return s.config.Agent
}

// Tabs returns the resolved tab configurations for this space.
func (s *Space) Tabs(prompt string) ([]config.Tab, error) {
	cs := s.configSpace()
	cs.Prompt = prompt
	return s.config.ResolveTabs(cs)
}

// newBackend constructs backends; replaced in tests.
var newBackend = backend.New

// Backend returns the session backend configured for this space.
func (s *Space) Backend() (backend.Backend, error) {
	return newBackend(s.config.Backend)
}

// backendSpace returns the identifiers a backend needs for this space.
func (s *Space) backendSpace() backend.Space {
	return backend.Space{Name: s.Name, Path: s.Path, RepoRoot: s.RepoRoot}
}

// EnvFile returns the absolute path of the configured env file, or "" if none.
func (s *Space) EnvFile() string {
	if s.config.EnvFile == "" {
		return ""
	}
	if filepath.IsAbs(s.config.EnvFile) {
		return s.config.EnvFile
	}
	return filepath.Join(s.Path, s.config.EnvFile)
}

// SessionActive reports whether the space has a live backend session.
func (s *Space) SessionActive() (bool, error) {
	b, err := s.Backend()
	if err != nil {
		return false, err
	}
	return b.Exists(s.backendSpace())
}

// CloseSession ends the space's backend session, keeping the worktree.
func (s *Space) CloseSession() error {
	b, err := s.Backend()
	if err != nil {
		return err
	}
	return b.Close(s.backendSpace())
}
