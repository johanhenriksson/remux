package spaces

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const registryFile = "spaces.yaml"

// Space represents a tracked workspace.
type Space struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Registry holds a list of tracked spaces.
type Registry struct {
	Spaces []Space `yaml:"spaces"`
}

// Load reads the space registry from the given directory.
// Returns an empty registry if the file doesn't exist.
func Load(dir string) (*Registry, error) {
	path := filepath.Join(dir, registryFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{}, nil
		}
		return nil, err
	}

	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return &reg, nil
}

// Save writes the registry to the given directory.
func (r *Registry) Save(dir string) error {
	path := filepath.Join(dir, registryFile)
	data, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Add adds a space to the registry. Idempotent - updates path if name exists.
func (r *Registry) Add(name, path string) {
	for i, s := range r.Spaces {
		if s.Name == name {
			r.Spaces[i].Path = path
			return
		}
	}
	r.Spaces = append(r.Spaces, Space{Name: name, Path: path})
}

// Remove removes a space by name.
func (r *Registry) Remove(name string) {
	for i, s := range r.Spaces {
		if s.Name == name {
			r.Spaces = append(r.Spaces[:i], r.Spaces[i+1:]...)
			return
		}
	}
}

// List returns all tracked spaces.
func (r *Registry) List() []Space {
	return r.Spaces
}
