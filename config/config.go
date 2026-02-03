package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configFile = ".remux.yaml"

// Config represents a workspace configuration file.
type Config struct {
	Env   map[string]string `yaml:"env"`
	Hooks Hooks             `yaml:"hooks"`
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
func Load(workspacePath string) (*Config, error) {
	path := filepath.Join(workspacePath, configFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
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
