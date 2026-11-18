package config_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/remux/config"
)

var _ = Describe("Instructions", func() {
	space := config.NewSpace("repo-feat", "/wt/repo-feat", 11020, "/repo")

	render := func(cfg *config.Config, env map[string]string) string {
		text, err := cfg.ResolveInstructions(space, env)
		Expect(err).NotTo(HaveOccurred())
		return text
	}

	Describe("ResolveInstructions", func() {
		It("renders the built-in base by default", func() {
			text := render(&config.Config{}, nil)
			Expect(text).To(HavePrefix("You are running in a remux workspace"))
			Expect(text).To(ContainSubstring("- Workspace: repo-feat at /wt/repo-feat\n"))
			Expect(text).To(ContainSubstring("- Main checkout: /repo (do not modify it)\n"))
			Expect(text).NotTo(ContainSubstring("{{"))
			Expect(text).NotTo(ContainSubstring("11020"))
		})

		It("appends project instructions after a blank line", func() {
			text := render(&config.Config{Instructions: "Run tests with make test on {{ space.Name }}.\n"}, nil)
			Expect(text).To(HaveSuffix("operations.\n\nRun tests with make test on repo-feat."))
		})

		It("omits the base when default_instructions is false", func() {
			off := false
			text := render(&config.Config{DefaultInstructions: &off, Instructions: "Project only."}, nil)
			Expect(text).To(Equal("Project only."))
		})

		It("returns nothing when the base is off and there are no instructions", func() {
			off := false
			Expect(render(&config.Config{DefaultInstructions: &off}, nil)).To(Equal(""))
		})

		It("exposes the space env to {{ env.X }}, overlaid on the host env", func() {
			GinkgoT().Setenv("REMUX_TEST_HOST", "host")
			GinkgoT().Setenv("API_PORT", "1")
			cfg := &config.Config{Instructions: "API on {{ env.API_PORT }}, {{ env.REMUX_TEST_HOST }}"}
			text := render(cfg, map[string]string{"API_PORT": "11021"})
			Expect(text).To(HaveSuffix("API on 11021, host"))
		})

		It("does not break on {{ prompt }}", func() {
			Expect(render(&config.Config{Instructions: "[{{ prompt }}]"}, nil)).To(HaveSuffix("[]"))
		})

		It("returns template errors", func() {
			_, err := (&config.Config{Instructions: "{{ nope.x }}"}).ResolveInstructions(space, nil)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("merge", func() {
		var tmpDir string

		BeforeEach(func() {
			tmpDir = GinkgoT().TempDir()
		})

		load := func(base, local string) *config.Config {
			Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.yaml"), []byte(base), 0644)).To(Succeed())
			if local != "" {
				Expect(os.WriteFile(filepath.Join(tmpDir, ".remux.local.yaml"), []byte(local), 0644)).To(Succeed())
			}
			cfg, err := config.Load(tmpDir, tmpDir)
			Expect(err).NotTo(HaveOccurred())
			return cfg
		}

		It("replaces instructions when the local config sets them", func() {
			cfg := load("instructions: repo text\n", "instructions: local text\n")
			Expect(cfg.Instructions).To(Equal("local text"))
		})

		It("keeps the repo's instructions when the local config omits them", func() {
			cfg := load("instructions: repo text\n", "agent: codex\n")
			Expect(cfg.Instructions).To(Equal("repo text"))
			Expect(cfg.DefaultInstructionsEnabled()).To(BeTrue())
		})

		It("lets the local config disable the base", func() {
			cfg := load("instructions: repo text\n", "default_instructions: false\n")
			Expect(cfg.DefaultInstructionsEnabled()).To(BeFalse())
			Expect(cfg.Instructions).To(Equal("repo text"))
		})

		It("lets the local config re-enable the base", func() {
			cfg := load("default_instructions: false\n", "default_instructions: true\n")
			Expect(cfg.DefaultInstructionsEnabled()).To(BeTrue())
		})

		It("keeps the repo's opt-out when the local config omits it", func() {
			cfg := load("default_instructions: false\n", "agent: codex\n")
			Expect(cfg.DefaultInstructionsEnabled()).To(BeFalse())
		})
	})
})
