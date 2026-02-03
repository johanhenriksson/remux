package registry_test

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/remux/registry"
)

func TestRegistry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Registry Suite")
}

var _ = Describe("Registry", func() {
	var (
		reg     *registry.Registry
		tempDir string
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "test-registry-*")
		Expect(err).NotTo(HaveOccurred())
		reg = &registry.Registry{}
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	Describe("AllocatePort", func() {
		It("returns BasePort for empty registry", func() {
			Expect(reg.AllocatePort()).To(Equal(registry.BasePort))
		})

		It("returns next port after single space", func() {
			reg.Add("space1", "/path/1", registry.BasePort, "/repo/root")
			Expect(reg.AllocatePort()).To(Equal(registry.BasePort + registry.PortRange))
		})

		It("returns max port + PortRange for multiple spaces", func() {
			reg.Add("space1", "/path/1", 11010, "/repo/root")
			reg.Add("space2", "/path/2", 11020, "/repo/root")
			reg.Add("space3", "/path/3", 11030, "/repo/root")
			Expect(reg.AllocatePort()).To(Equal(11040))
		})

		It("handles non-sequential ports", func() {
			reg.Add("space1", "/path/1", 11010, "/repo/root")
			reg.Add("space2", "/path/2", 11050, "/repo/root") // gap
			Expect(reg.AllocatePort()).To(Equal(11060))
		})
	})

	Describe("Get", func() {
		It("returns nil for non-existent space", func() {
			Expect(reg.Get("missing")).To(BeNil())
		})

		It("returns pointer to existing entry", func() {
			reg.Add("test", "/path/test", 11010, "/repo/root")
			entry := reg.Get("test")
			Expect(entry).NotTo(BeNil())
			Expect(entry.Name).To(Equal("test"))
			Expect(entry.Port).To(Equal(11010))
		})
	})

	Describe("Add", func() {
		It("adds new entry with port", func() {
			reg.Add("new", "/path/new", 11010, "/repo/root")
			Expect(reg.List()).To(HaveLen(1))
			Expect(reg.List()[0].Port).To(Equal(11010))
		})

		It("updates existing entry", func() {
			reg.Add("test", "/old/path", 11010, "/repo/root")
			reg.Add("test", "/new/path", 11020, "/repo/root2")
			Expect(reg.List()).To(HaveLen(1))
			Expect(reg.List()[0].Path).To(Equal("/new/path"))
			Expect(reg.List()[0].Port).To(Equal(11020))
		})
	})

	Describe("Save and Load", func() {
		It("persists port and repo_root fields", func() {
			reg.Add("test", "/path/test", 11010, "/repo/root")
			err := reg.Save(tempDir)
			Expect(err).NotTo(HaveOccurred())

			loaded, err := registry.Load(tempDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.List()).To(HaveLen(1))
			Expect(loaded.List()[0].Port).To(Equal(11010))
			Expect(loaded.List()[0].RepoRoot).To(Equal("/repo/root"))
		})
	})
})
