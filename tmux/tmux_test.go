package tmux_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/tmux"
)

func TestTmux(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tmux Suite")
}

var _ = Describe("Tmux", func() {
	Describe("SessionName", func() {
		It("replaces dots with underscores", func() {
			Expect(tmux.SessionName("my.workspace")).To(Equal("my_workspace"))
		})

		It("replaces colons with underscores", func() {
			Expect(tmux.SessionName("my:workspace")).To(Equal("my_workspace"))
		})

		It("replaces multiple special characters", func() {
			Expect(tmux.SessionName("repo.name:branch")).To(Equal("repo_name_branch"))
		})

		It("leaves valid names unchanged", func() {
			Expect(tmux.SessionName("my-workspace")).To(Equal("my-workspace"))
		})
	})
})
