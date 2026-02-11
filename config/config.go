package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configFile = ".remux.yaml"
const localConfigFile = ".remux.local.yaml"

// Tab represents a tmux window/tab configuration.
type Tab struct {
	Name string `yaml:"name"`
	Cmd  string `yaml:"cmd"`
}

// Config represents a workspace configuration file.
type Config struct {
	Env   map[string]string `yaml:"env"`
	Hooks Hooks             `yaml:"hooks"`
	Tabs  []Tab             `yaml:"tabs"`
}

// Hooks contains lifecycle hook commands.
type Hooks struct {
	OnCreate []string `yaml:"on_create"`
	OnOpen   []string `yaml:"on_open"`
	OnDrop   []string `yaml:"on_drop"`
}

// Space provides template variables for expression evaluation.
type Space struct {
	Name     string
	Path     string
	Port     int
	ID       string
	RepoRoot string
}

// NewSpace creates a Space from the given values, computing the ID automatically.
func NewSpace(name, path string, port int, repoRoot string) Space {
	return Space{
		Name:     name,
		Path:     path,
		Port:     port,
		ID:       strings.ReplaceAll(name, "-", "_"),
		RepoRoot: repoRoot,
	}
}

// Load reads a config file from the workspace directory.
// Returns a default empty config if the file doesn't exist.
// If a .remux.local.yaml file exists, it is merged on top of the base config.
func Load(workspacePath string) (*Config, error) {
	base, err := loadFile(filepath.Join(workspacePath, configFile))
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = &Config{}
	}

	local, err := loadFile(filepath.Join(workspacePath, localConfigFile))
	if err != nil {
		return nil, err
	}

	if local != nil {
		base = merge(base, local)
	}

	return base, nil
}

// loadFile reads and parses a single YAML config file.
// Returns nil (without error) if the file doesn't exist.
func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// merge returns a new Config combining base and override.
// Env: maps are merged (override keys win, base-only keys preserved).
// Tabs: replaced entirely if override defines any.
// Hooks: replaced per hook type (on_create, on_open, on_drop are independent).
func merge(base, override *Config) *Config {
	result := *base

	// Merge env maps
	if len(override.Env) > 0 {
		merged := make(map[string]string, len(base.Env)+len(override.Env))
		for k, v := range base.Env {
			merged[k] = v
		}
		for k, v := range override.Env {
			merged[k] = v
		}
		result.Env = merged
	}

	// Replace tabs entirely
	if len(override.Tabs) > 0 {
		result.Tabs = override.Tabs
	}

	// Replace hooks per type
	if len(override.Hooks.OnCreate) > 0 {
		result.Hooks.OnCreate = override.Hooks.OnCreate
	}
	if len(override.Hooks.OnOpen) > 0 {
		result.Hooks.OnOpen = override.Hooks.OnOpen
	}
	if len(override.Hooks.OnDrop) > 0 {
		result.Hooks.OnDrop = override.Hooks.OnDrop
	}

	return &result
}

// ResolveEnv evaluates template expressions in env vars and returns resolved values.
func (c *Config) ResolveEnv(space Space) (map[string]string, error) {
	if len(c.Env) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(c.Env))
	for key, value := range c.Env {
		resolved, err := EvaluateTemplate(value, space)
		if err != nil {
			return nil, err
		}
		result[key] = resolved
	}
	return result, nil
}

// RunOnCreate executes on_create hooks. Prints warnings on failure, does not return error.
func (c *Config) RunOnCreate(space Space) {
	if len(c.Hooks.OnCreate) == 0 {
		return
	}
	env, err := c.ResolveEnv(space)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: on_create hook failed to resolve env: %v\n", err)
		return
	}
	if err := runHooks(c.Hooks.OnCreate, space, space.Path, env); err != nil {
		fmt.Fprintf(os.Stderr, "warning: on_create hook failed: %v\n", err)
	}
}

// RunOnOpen executes on_open hooks. Returns error on failure.
func (c *Config) RunOnOpen(space Space) error {
	if len(c.Hooks.OnOpen) == 0 {
		return nil
	}
	env, err := c.ResolveEnv(space)
	if err != nil {
		return fmt.Errorf("on_open hook failed to resolve env: %w", err)
	}
	if err := runHooks(c.Hooks.OnOpen, space, space.Path, env); err != nil {
		return fmt.Errorf("on_open hook failed: %w", err)
	}
	return nil
}

// RunOnDrop executes on_drop hooks. Returns error on failure.
func (c *Config) RunOnDrop(space Space) error {
	if len(c.Hooks.OnDrop) == 0 {
		return nil
	}
	env, err := c.ResolveEnv(space)
	if err != nil {
		return fmt.Errorf("on_drop hook failed to resolve env: %w", err)
	}
	if err := runHooks(c.Hooks.OnDrop, space, space.Path, env); err != nil {
		return fmt.Errorf("on_drop hook failed: %w", err)
	}
	return nil
}

// ResolveTabs evaluates template expressions in tab names and commands.
func (c *Config) ResolveTabs(space Space) ([]Tab, error) {
	if len(c.Tabs) == 0 {
		return nil, nil
	}

	result := make([]Tab, len(c.Tabs))
	for i, tab := range c.Tabs {
		name, err := EvaluateTemplate(tab.Name, space)
		if err != nil {
			return nil, fmt.Errorf("tab %d name: %w", i, err)
		}
		cmd, err := EvaluateTemplate(tab.Cmd, space)
		if err != nil {
			return nil, fmt.Errorf("tab %d cmd: %w", i, err)
		}
		result[i] = Tab{Name: name, Cmd: cmd}
	}
	return result, nil
}
