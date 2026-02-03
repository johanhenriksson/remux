package spaces

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/registry"
)

// Space represents a loaded workspace with config.
type Space struct {
	Name     string
	Path     string
	Port     int
	RepoRoot string
	config   *config.Config
}

// ID returns a sanitized identifier for the space (hyphens replaced with underscores).
func (s *Space) ID() string {
	return strings.ReplaceAll(s.Name, "-", "_")
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

	cfg, err := config.Load(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	space := &Space{
		Name:     entry.Name,
		Path:     entry.Path,
		Port:     entry.Port,
		RepoRoot: entry.RepoRoot,
		config:   cfg,
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
