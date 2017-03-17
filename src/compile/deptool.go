package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudfoundry/libbuildpack"
)

type DepTool interface {
	Name() string
	GoVersion() string
	PackageName() string
}

type godep struct {
	data           map[string]string
	defaultVersion string
}

func NewDepTool(dir string, defaultVersion string) (DepTool, error) {
	if _, err := os.Stat(filepath.Join(dir, "Godeps", "Godeps.json")); !os.IsNotExist(err) {
		tool := godep{defaultVersion: defaultVersion}
		err := libbuildpack.NewJSON().Load(filepath.Join(dir, "Godeps", "Godeps.json"), &tool.data)
		return tool, err
	}

	return nil, errors.New("Could not determine tool")
}

func convertVersion(inp string) string {
	inp = strings.TrimPrefix(inp, "go")
	if inp != "" {
		switch strings.Count(inp, ".") {
		case 0:
			inp = inp + ".x.x"
		case 1:
			inp = inp + ".x"
		}
	}
	return inp
}

func (d godep) Name() string { return "godep" }
func (d godep) GoVersion() string {
	if d.data["GoVersion"] != "" {
		return convertVersion(d.data["GoVersion"])
	} else if os.Getenv("GOVERSION") != "" {
		return convertVersion(os.Getenv("GOVERSION"))
	} else {
		return convertVersion(d.defaultVersion)
	}
}
func (d godep) PackageName() string {
	return d.data["ImportPath"]
}
