package config_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/remux/config"
)

var _ = Describe("Config", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "config-test")
		Expect(err).NotTo(HaveOccurred())
		// Resolve symlinks for consistent path comparison (macOS /var -> /private/var)
		tmpDir, err = filepath.EvalSymlinks(tmpDir)
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
			err := os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(content), 0644)
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

		It("loads tabs configuration", func() {
			content := `
tabs:
  - cmd: claude
  - cmd: nvim .
  - name: shell
`
			err := os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())
			Expect(cfg.Tabs).To(HaveLen(3))
			Expect(cfg.Tabs[0]).To(Equal(config.Tab{Cmd: "claude"}))
			Expect(cfg.Tabs[1]).To(Equal(config.Tab{Cmd: "nvim ."}))
			Expect(cfg.Tabs[2]).To(Equal(config.Tab{Name: "shell"}))
		})

		It("returns error for invalid YAML", func() {
			content := `env: [invalid`
			err := os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg, err := config.Load(tmpDir)
			Expect(err).To(HaveOccurred())
			Expect(cfg).To(BeNil())
		})
	})

	Describe("Local config merge", func() {
		It("merges env vars with local overriding base", func() {
			base := "env:\n  FOO: base\n  BAR: base_only\n"
			local := "env:\n  FOO: local\n  BAZ: local_only\n"
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(base), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.local.yaml"), []byte(local), 0644)).To(Succeed())

			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Env).To(HaveKeyWithValue("FOO", "local"))
			Expect(cfg.Env).To(HaveKeyWithValue("BAR", "base_only"))
			Expect(cfg.Env).To(HaveKeyWithValue("BAZ", "local_only"))
		})

		It("replaces tabs when local defines them", func() {
			base := "tabs:\n  - cmd: base-cmd\n"
			local := "tabs:\n  - cmd: local-cmd\n  - cmd: local-cmd-2\n"
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(base), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.local.yaml"), []byte(local), 0644)).To(Succeed())

			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Tabs).To(HaveLen(2))
			Expect(cfg.Tabs[0].Cmd).To(Equal("local-cmd"))
			Expect(cfg.Tabs[1].Cmd).To(Equal("local-cmd-2"))
		})

		It("replaces individual hook types while keeping others from base", func() {
			base := "hooks:\n  on_create:\n    - base-create\n  on_open:\n    - base-open\n  on_drop:\n    - base-drop\n"
			local := "hooks:\n  on_open:\n    - local-open\n"
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(base), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.local.yaml"), []byte(local), 0644)).To(Succeed())

			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Hooks.OnCreate).To(Equal([]string{"base-create"}))
			Expect(cfg.Hooks.OnOpen).To(Equal([]string{"local-open"}))
			Expect(cfg.Hooks.OnDrop).To(Equal([]string{"base-drop"}))
		})

		It("has no effect when local config is missing", func() {
			base := "env:\n  FOO: bar\ntabs:\n  - cmd: test\n"
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(base), 0644)).To(Succeed())

			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Env).To(HaveKeyWithValue("FOO", "bar"))
			Expect(cfg.Tabs).To(HaveLen(1))
		})

		It("leaves base fields intact when local only sets some fields", func() {
			base := "env:\n  FOO: bar\ntabs:\n  - cmd: base-cmd\nhooks:\n  on_create:\n    - base-create\n"
			local := "env:\n  BAZ: local\n"
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(base), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.local.yaml"), []byte(local), 0644)).To(Succeed())

			cfg, err := config.Load(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Env).To(HaveKeyWithValue("FOO", "bar"))
			Expect(cfg.Env).To(HaveKeyWithValue("BAZ", "local"))
			Expect(cfg.Tabs).To(HaveLen(1))
			Expect(cfg.Tabs[0].Cmd).To(Equal("base-cmd"))
			Expect(cfg.Hooks.OnCreate).To(Equal([]string{"base-create"}))
		})
	})

	Describe("Hooks", func() {
		It("receives resolved env vars", func() {
			outputFile := filepath.Join(tmpDir, "env_output.txt")
			cfg := &config.Config{
				Env: map[string]string{
					"TEST_VAR": "{{ space.Port }}",
				},
				Hooks: config.Hooks{
					OnOpen: []string{"echo $TEST_VAR > " + outputFile},
				},
			}

			space := config.NewSpace("test-space", tmpDir, 12345, tmpDir)
			err := cfg.RunOnOpen(space)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(outputFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(string(content))).To(Equal("12345"))
		})

		It("runs in the correct working directory", func() {
			outputFile := filepath.Join(tmpDir, "pwd_output.txt")
			cfg := &config.Config{
				Hooks: config.Hooks{
					OnOpen: []string{"pwd > " + outputFile},
				},
			}

			space := config.NewSpace("test-space", tmpDir, 11000, tmpDir)
			err := cfg.RunOnOpen(space)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(outputFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(string(content))).To(Equal(tmpDir))
		})

		It("supports shell features", func() {
			outputFile := filepath.Join(tmpDir, "shell_output.txt")
			cfg := &config.Config{
				Hooks: config.Hooks{
					OnOpen: []string{"echo test || true && echo success > " + outputFile},
				},
			}

			space := config.NewSpace("test-space", tmpDir, 11000, tmpDir)
			err := cfg.RunOnOpen(space)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(outputFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(string(content))).To(Equal("success"))
		})

		It("inherits parent environment", func() {
			outputFile := filepath.Join(tmpDir, "parent_env_output.txt")
			os.Setenv("REMUX_TEST_PARENT_VAR", "inherited_value")
			defer os.Unsetenv("REMUX_TEST_PARENT_VAR")

			cfg := &config.Config{
				Hooks: config.Hooks{
					OnOpen: []string{"echo $REMUX_TEST_PARENT_VAR > " + outputFile},
				},
			}

			space := config.NewSpace("test-space", tmpDir, 11000, tmpDir)
			err := cfg.RunOnOpen(space)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(outputFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(string(content))).To(Equal("inherited_value"))
		})
	})

	Describe("ResolveEnv", func() {
		It("resolves template expressions", func() {
			cfg := &config.Config{
				Env: map[string]string{
					"PORT":      "{{ space.Port }}",
					"NEXT_PORT": "{{ space.Port + 1 }}",
					"DB_NAME":   "app_{{ space.ID }}",
					"STATIC":    "no_template",
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

	Describe("ResolveTabs", func() {
		It("resolves template expressions in tabs", func() {
			cfg := &config.Config{
				Tabs: []config.Tab{
					{Name: "editor", Cmd: "nvim ."},
					{Name: "{{ space.Name }}", Cmd: "echo {{ space.Port }}"},
					{Cmd: "shell"},
				},
			}

			ctx := config.Space{
				Name: "my-space",
				Port: 11010,
			}

			tabs, err := cfg.ResolveTabs(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(tabs).To(HaveLen(3))
			Expect(tabs[0]).To(Equal(config.Tab{Name: "editor", Cmd: "nvim ."}))
			Expect(tabs[1]).To(Equal(config.Tab{Name: "my-space", Cmd: "echo 11010"}))
			Expect(tabs[2]).To(Equal(config.Tab{Name: "", Cmd: "shell"}))
		})

		It("returns nil for empty tabs", func() {
			cfg := &config.Config{}
			tabs, err := cfg.ResolveTabs(config.Space{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tabs).To(BeNil())
		})

		It("returns error for invalid expression", func() {
			cfg := &config.Config{
				Tabs: []config.Tab{
					{Name: "{{ invalid.field }}"},
				},
			}

			_, err := cfg.ResolveTabs(config.Space{})
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Template", func() {
	Describe("EvaluateTemplate", func() {
		ctx := config.Space{
			Name:     "test-space",
			Path:     "/path/to/space",
			Port:     11020,
			ID:       "test_space",
			RepoRoot: "/repo/root",
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

		It("evaluates RepoRoot expression", func() {
			result, err := config.EvaluateTemplate("{{ space.RepoRoot }}/scripts/setup.sh", ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/repo/root/scripts/setup.sh"))
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
