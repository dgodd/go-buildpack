package main_test

import (
	c "compile"
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = FDescribe("DepTool", func() {
	var (
		err      error
		buildDir string

		tool c.DepTool
	)

	BeforeEach(func() {
		buildDir, err = ioutil.TempDir("", "build")
		Expect(err).To(BeNil())
	})

	Context("godeps", func() {
		Context("invalid json", func() {
			It("returns error", func() {
				os.MkdirAll(filepath.Join(buildDir, "Godeps"), 0755)
				ioutil.WriteFile(filepath.Join(buildDir, "Godeps", "Godeps.json"), []byte("invalid"), 0644)
				_, err = c.NewDepTool(buildDir, "go1.7")
				Expect(err).ToNot(BeNil())
			})
		})

		Context("valid json", func() {
			var godepsJson, defaultVersion string
			BeforeEach(func() {
				godepsJson = "{}"
				defaultVersion = "1.8.2"
			})
			JustBeforeEach(func() {
				os.MkdirAll(filepath.Join(buildDir, "Godeps"), 0755)
				ioutil.WriteFile(filepath.Join(buildDir, "Godeps", "Godeps.json"), []byte(godepsJson), 0644)
				tool, err = c.NewDepTool(buildDir, defaultVersion)
				Expect(err).To(BeNil())
			})
			Describe("Name", func() {
				It("== godep", func() {
					Expect(tool.Name()).To(Equal("godep"))
				})
			})

			Describe("GoVersion", func() {
				Context("Godeps.json contains go1.7", func() {
					BeforeEach(func() { godepsJson = "{\"GoVersion\":\"go1.7\"}" })
					It("== 1.7.x", func() {
						Expect(tool.GoVersion()).To(Equal("1.7.x"))
					})
				})
				Context("Godeps.json contains go1.7.3", func() {
					BeforeEach(func() { godepsJson = "{\"GoVersion\":\"go1.7.3\"}" })
					It("== 1.7.3", func() {
						Expect(tool.GoVersion()).To(Equal("1.7.3"))
					})
				})
				Context("Godeps.json does NOT contain version", func() {
					Context("ENV[GOVERSION] is set", func() {
						BeforeEach(func() { os.Setenv("GOVERSION", "1.3") })
						It("== ENV[GOVERSION]", func() {
							Expect(tool.GoVersion()).To(Equal("1.3.x"))
						})
					})
					Context("ENV[GOVERSION] is NOT set", func() {
						BeforeEach(func() { os.Unsetenv("GOVERSION") })
						It("== Default Go from manifest", func() {
							Expect(tool.GoVersion()).To(Equal("1.8.2"))
						})

						// TODO: Test GOVERSION warning. https://github.com/cloudfoundry/go-buildpack/blob/master/lib/common.sh#L57
					})
				})
			})
			Describe("PackageName", func() {
				BeforeEach(func() { godepsJson = "{\"ImportPath\":\"myname\"}" })
				It("== .ImportPath", func() {
					Expect(tool.PackageName()).To(Equal("myname"))
				})
			})
		})
	})
})
