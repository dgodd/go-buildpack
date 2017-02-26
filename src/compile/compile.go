package main

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	bp "github.com/cloudfoundry/libbuildpack"
)

type GoCompiler struct {
	Compiler *bp.Compiler
	Json     bp.JSON
	Yaml     bp.YAML
}

func main() {
	buildDir := os.Args[1]
	cacheDir := os.Args[2]

	compiler, err := bp.NewCompiler(buildDir, cacheDir, bp.NewLogger())
	err = compiler.CheckBuildpackValid()
	if err != nil {
		panic(err)
	}

	goCompiler := GoCompiler{Compiler: compiler, Json: bp.NewJSON(), Yaml: bp.NewYAML()}
	err = goCompiler.Compile()
	if err != nil {
		panic(err)
	}

	compiler.StagingComplete()
}

func (c *GoCompiler) Compile() error {
	// $BUILDPACK_PATH/compile-extensions/bin/warn_if_newer_patch godep $manifest_file
	// $BUILDPACK_PATH/compile-extensions/bin/warn_if_newer_patch glide $manifest_file
	// $BUILDPACK_PATH/compile-extensions/bin/warn_if_newer_patch go $manifest_file

	// loadEnvDir() // REALLY????

	tool, appName, goVersionStr, err := c.determineTool()
	if err != nil {
		c.Compiler.Log.Error("Could not determine go tool")
		return err
	}

	c.Compiler.Log.Warning("tool=%s, appName=%s, goVersion=%s", tool, appName, goVersionStr)
	goVersion := strings.TrimPrefix(goVersionStr, "go")
	goInstallPath := filepath.Join(c.Compiler.CacheDir, goVersionStr)
	goRoot := filepath.Join(goInstallPath, "go")
	goBinFile := filepath.Join(goRoot, "bin", "go")
	if fileExists(goBinFile) {
		c.Compiler.Log.BeginStep("Re-using go version %s", goVersion)
	} else {
		c.Compiler.Log.BeginStep("Installing go version %s", goVersion)
		os.RemoveAll(goInstallPath)
		err = c.Compiler.Manifest.InstallDependency(bp.Dependency{Name: "go", Version: goVersion}, goInstallPath)
		if err != nil {
			c.Compiler.Log.Error("Could not install go version %s", goVersion)
			return err
		}
	}

	// Go Native Vendoring
	// setupGOPATH() ; Handle mv option (GO_SETUP_GOPATH_IN_IMAGE)
	dir, err := ioutil.TempDir("", "gopath")
	goPath := filepath.Join(dir, ".go")
	err = os.MkdirAll(goPath, 0755)
	goBin := filepath.Join(c.Compiler.BuildDir, "bin")
	err = os.MkdirAll(goBin, 0755)

	packageDir := filepath.Join(goPath, "src", appName)
	err = os.MkdirAll(filepath.Join(goPath, "src"), 0755)
	if err != nil {
		c.Compiler.Log.Error("Could not make %s", packageDir)
		return err
	}
	err = CopyDir(c.Compiler.BuildDir, packageDir)
	if err != nil {
		c.Compiler.Log.Error("Could not copy %s to %s", c.Compiler.BuildDir, packageDir)
		return err
	}

	pkgs := os.Getenv("GO_INSTALL_PACKAGE_SPEC")
	if pkgs == "" || pkgs == "default" {
		c.Compiler.Log.Warning("Installing package '.' (default)")
		pkgs = "."
	}

	if os.Getenv("GO15VENDOREXPERIMENT") == "0" {
		c.Compiler.Log.Warning("$GO15VENDOREXPERIMENT=0. To use vendor your packages in vendor\nfor go 1.6 this environment variable must unset or set to 1.")
		return errors.New("$GO15VENDOREXPERIMENT=0")
	}

	if tool == "glide" {
		if hasSubDirs(filepath.Join(c.Compiler.BuildDir, "vendor")) {
			c.Compiler.Log.BeginStep("Note: skipping (glide install) due to non-empty vendor directory.")
		} else {
			c.Compiler.Log.BeginStep("Fetching any unsaved dependencies (glide install)")
			cmd := exec.Command("/tmp/bin/glide", "install")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stdout
			cmd.Dir = packageDir
			cmd.Env = []string{
				"GOROOT=" + goRoot,
				"GOPATH=" + goPath,
				"GOBIN=" + goBin,
			}
			if _, err := cmd.Output(); err != nil {
				c.Compiler.Log.Error("Could not run glide install")
				return err
			}
		}
	}

	// FIXME implement equivalent of
	// massagePkgSpecForVendor() // https://github.com/dgodd/go-buildpack/blob/golang/bin/compile.old#L52

	// step "Running: go install -v ${FLAGS[@]} ${pkgs}"
	c.Compiler.Log.BeginStep("Running: go install -v -tags cloudfoundry %s", pkgs)
	cmd := exec.Command(goBinFile, "install", "-v", "-tags", "cloudfoundry", pkgs)
	cmd.Dir = packageDir
	cmd.Env = []string{
		"GOROOT=" + goRoot,
		"GOPATH=" + goPath,
		"GOBIN=" + goBin,
	}
	if out, err := cmd.Output(); err != nil {
		c.Compiler.Log.Error("Could not compile app")
		c.Compiler.Log.Error(string(out))
		return err
	}

	release_yml := "---\ndefault_process_types:\n  web: " + appName + "\n\n"
	err = ioutil.WriteFile("/tmp/buildpack-release-step.yml", []byte(release_yml), 0644)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Join(c.Compiler.BuildDir, ".profile.d"), 0755); err != nil {
		return err
	}
	if err = ioutil.WriteFile(filepath.Join(c.Compiler.BuildDir, ".profile.d", "go.sh"), []byte("PATH=$PATH:$HOME/bin"), 0644); err != nil {
		return err
	}

	if os.Getenv("GO_INSTALL_TOOLS_IN_IMAGE") == "true" {
		c.Compiler.Log.BeginStep("Copying go tool chain to $GOROOT=$HOME/.cloudfoundry/go")
		if err := os.MkdirAll(filepath.Join(c.Compiler.BuildDir, ".cloudfoundry"), 0755); err != nil {
			return err
		}
		if err := CopyDir(goRoot, filepath.Join(c.Compiler.BuildDir, ".cloudfoundry", "go")); err != nil {
			c.Compiler.Log.Error("Could not copy %s to %s", goRoot, filepath.Join(c.Compiler.BuildDir, ".cloudfoundry", "go"))
			return err
		}
		if err = ioutil.WriteFile(filepath.Join(c.Compiler.BuildDir, ".profile.d", "goroot.sh"), []byte("export GOROOT=$HOME/.cloudfoundry/go\nPATH=$PATH:$GOROOT/bin\n"), 0644); err != nil {
			return err
		}
		// FIXME do the below
		// c.Compiler.Log.BeginStep("Copying ${TOOL} binary")
		// cp $(which ${TOOL}) "${build}/bin"
	}

	if os.Getenv("GO_SETUP_GOPATH_IN_IMAGE") == "true" {
		c.Compiler.Log.BeginStep("Cleaning up $GOPATH/pkg")
		// FIXME Why does the below exist??
		os.RemoveAll(filepath.Join(goPath, "pkg"))
		if err = ioutil.WriteFile(filepath.Join(c.Compiler.BuildDir, ".profile.d", "zzgopath.sh"), []byte("export GOPATH=$HOME\ncd $GOPATH/src/"+appName+"\n"), 0644); err != nil {
			return err
		}
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// Returns tool, appName, goVersion, error
func (c *GoCompiler) determineTool() (string, string, string, error) {
	godepsJSON := filepath.Join(c.Compiler.BuildDir, "Godeps", "Godeps.json")
	if fileExists(godepsJSON) {
		c.Compiler.Log.BeginStep("Checking Godeps/Godeps.json file.")
		hash := make(map[string]string)
		err := c.Json.Load(godepsJSON, &hash)
		if err != nil {
			c.Compiler.Log.Error("Bad Godeps/Godeps.json file")
			return "", "", "", err
		}
		goVersion := hash["GoVersion"]
		if os.Getenv("GOVERSION") != "" && os.Getenv("GOVERSION") != goVersion {
			goVersion = os.Getenv("GOVERSION")
			c.Compiler.Log.Warning("Using $GOVERSION override.\n      $GOVERSION = ${GOVERSION}\n\nIf this isn't what you want please run:'\n   cf unset-env <app> GOVERSION\n\n")
		}
		return "godep", hash["ImportPath"], goVersion, nil
	}

	goVersion := os.Getenv("GOVERSION")
	if goVersion == "" {
		dep, err := c.Compiler.Manifest.DefaultVersion("go")
		if err != nil {
			c.Compiler.Log.Error("Could not find determine default go version")
			return "", "", "", errors.New("Could not find determine default go version")
		}
		goVersion = dep.Version
	}

	if fileExists(filepath.Join(c.Compiler.BuildDir, ".godir")) {
		c.Compiler.Log.Warning("Deprecated, .godir file found! Please update to supported Godep or Glide dependency managers.\nSee https://github.com/tools/godep or https://github.com/Masterminds/glide for usage information.")
		return "godir", "", goVersion, errors.New("GoDir is deprecated")
	}

	glideYaml := filepath.Join(c.Compiler.BuildDir, "glide.yaml")
	if fileExists(glideYaml) {
		dep, err := c.Compiler.Manifest.DefaultVersion("glide")
		if err != nil {
			c.Compiler.Log.Error("Could not find determine default glide version")
			return "", "", "", errors.New("Could not find determine default glide version")
		}
		err = c.Compiler.Manifest.InstallDependency(dep, "/tmp")
		if err != nil {
			c.Compiler.Log.Error("Could not install go version %s", goVersion)
			return "", "", "", err
		}

		// FIXME Reading glide.yaml directly instead of calling 'glade name'
		hash := new(struct {
			Package string `yaml:"package"`
		})
		err = c.Yaml.Load(glideYaml, &hash)
		if err != nil {
			c.Compiler.Log.Error("Bad glide.yaml file")
			return "", "", "", err
		}
		if hash.Package == "" {
			c.Compiler.Log.Error("Bad glide.yaml file, must have a package field")
			return "", "", "", err
		}

		return "glide", hash.Package, goVersion, nil
	}

	// elif (test -d "$build/src" && test -n "$(find "$build/src" -mindepth 2 -type f -name '*.go' | sed 1q)")
	// then
	// TOOL="gb"
	// ver=${GOVERSION:-$DefaultGoVersion}
	// else

	// Native Vendoring
	if os.Getenv("GOPACKAGENAME") == "" {
		c.Compiler.Log.Error("To use go native vendoring set the $GOPACKAGENAME\nenvironment variable to your app's package name")
		return "", "", "", errors.New("To use go native vendoring set the $GOPACKAGENAME environment variable to your app's package name")
	}
	return "go_nativevendoring", os.Getenv("GOPACKAGENAME"), goVersion, nil
}

func hasSubDirs(path string) bool {
	dirs, err := ioutil.ReadDir(path)
	if err != nil {
		return false
	}
	for _, info := range dirs {
		if info.Mode().IsDir() {
			return true
		}
	}
	return false
}
