package config_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/config"
)

var _ = Describe("Config", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "config-test")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("Load", func() {
		It("returns empty config when config file doesn't exist", func() {
			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())
			Expect(cfg.Env).To(BeNil())
			Expect(cfg.Hooks.OnCreate).To(BeNil())
		})

		It("loads a valid config file", func() {
			content := `
env:
  FOO: bar
  PORT: "8080"
hooks:
  on_create:
    - echo "creating"
  on_open:
    - echo "opening"
  on_drop:
    - echo "dropping"
`
			err := os.WriteFile(filepath.Join(tmpDir, ".aut.yaml"), []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())
			Expect(cfg.Env).To(HaveKeyWithValue("FOO", "bar"))
			Expect(cfg.Env).To(HaveKeyWithValue("PORT", "8080"))
			Expect(cfg.Hooks.OnCreate).To(Equal([]string{`echo "creating"`}))
			Expect(cfg.Hooks.OnOpen).To(Equal([]string{`echo "opening"`}))
			Expect(cfg.Hooks.OnDrop).To(Equal([]string{`echo "dropping"`}))
		})

		It("returns error for invalid YAML", func() {
			content := `env: [invalid`
			err := os.WriteFile(filepath.Join(tmpDir, ".aut.yaml"), []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg, err := config.Load(tmpDir)
			Expect(err).To(HaveOccurred())
			Expect(cfg).To(BeNil())
		})
	})

	Describe("ResolveEnv", func() {
		It("resolves template expressions", func() {
			cfg := &config.Config{
				Env: map[string]string{
					"PORT":     "{{ space.Port }}",
					"NEXT_PORT": "{{ space.Port + 1 }}",
					"DB_NAME":  "app_{{ space.ID }}",
					"STATIC":   "no_template",
				},
			}

			ctx := config.Space{
				Name: "my-feature",
				Path: "/path/to/workspace",
				Port: 11010,
				ID:   "my_feature",
			}

			resolved, err := cfg.ResolveEnv(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved).To(HaveKeyWithValue("PORT", "11010"))
			Expect(resolved).To(HaveKeyWithValue("NEXT_PORT", "11011"))
			Expect(resolved).To(HaveKeyWithValue("DB_NAME", "app_my_feature"))
			Expect(resolved).To(HaveKeyWithValue("STATIC", "no_template"))
		})

		It("returns nil for empty env", func() {
			cfg := &config.Config{}
			resolved, err := cfg.ResolveEnv(config.Space{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved).To(BeNil())
		})

		It("returns error for invalid expression", func() {
			cfg := &config.Config{
				Env: map[string]string{
					"BAD": "{{ invalid.field }}",
				},
			}

			_, err := cfg.ResolveEnv(config.Space{})
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Template", func() {
	Describe("EvaluateTemplate", func() {
		ctx := config.Space{
			Name: "test-space",
			Path: "/path/to/space",
			Port: 11020,
			ID:   "test_space",
		}

		It("evaluates simple expressions", func() {
			result, err := config.EvaluateTemplate("Port is {{ space.Port }}", ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("Port is 11020"))
		})

		It("evaluates arithmetic expressions", func() {
			result, err := config.EvaluateTemplate("{{ space.Port + 100 }}", ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("11120"))
		})

		It("evaluates string expressions", func() {
			result, err := config.EvaluateTemplate("db_{{ space.ID }}", ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("db_test_space"))
		})

		It("handles multiple expressions", func() {
			result, err := config.EvaluateTemplate("{{ space.Name }} on port {{ space.Port }}", ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("test-space on port 11020"))
		})

		It("returns string unchanged when no templates", func() {
			result, err := config.EvaluateTemplate("no templates here", ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("no templates here"))
		})

		It("returns error for invalid expression", func() {
			_, err := config.EvaluateTemplate("{{ unknown.field }}", ctx)
			Expect(err).To(HaveOccurred())
		})
	})
})
