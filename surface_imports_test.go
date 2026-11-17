package ginject_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

const modulePath = "github.com/dangduoc08/ginject"

var movedPackages = map[string]bool{
	"common":       true,
	"ctx":          true,
	"core":         true,
	"pattern":      true,
	"devtool":      true,
	"wsevent":      true,
	"routing":      true,
	"memorybroker": true,
	"memorycache":  true,
	"accesslog":    true,
	"trace":        true,
	"aggregation":  true,
	"versioning":   true,
}

func forbiddenImport(path string) bool {
	if !strings.HasPrefix(path, modulePath+"/") {
		return false
	}
	elements := strings.Split(strings.TrimPrefix(path, modulePath+"/"), "/")
	for _, element := range elements {
		if element == "internal" {
			return true
		}
	}
	return movedPackages[elements[0]]
}

func collectGoFiles(t *testing.T, root string, recursive bool) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && !recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSurface_SampleUsesPublicImportsOnly(t *testing.T) {
	files := collectGoFiles(t, "sample", true)
	for _, file := range collectGoFiles(t, "bench", false) {
		if !strings.HasSuffix(file, "_test.go") {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		t.Fatal(test.DiffMessage(0, "some files", "B4: sample/ and bench/ have Go files to check"))
	}
	fset := token.NewFileSet()
	var offenders []string
	for _, file := range files {
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if forbiddenImport(path) {
				offenders = append(offenders, file+" imports "+path)
			}
		}
	}
	if len(offenders) != 0 {
		t.Error(test.DiffMessage(strings.Join(offenders, "\n"), "no internal or moved-package import", "B4: sample/ and bench/ import public paths only"))
	}
}

func TestSurface_ForbiddenImportRule(t *testing.T) {
	cases := map[string]bool{
		modulePath + "/internal/core":      true,
		modulePath + "/internal/test":      true,
		modulePath + "/core":               true,
		modulePath + "/common":             true,
		modulePath + "/ctx":                true,
		modulePath + "/trace":              true,
		modulePath + "/log":                false,
		modulePath + "/exception":          false,
		modulePath + "/middlewares/helmet": false,
		modulePath + "/modules/cache":      false,
		modulePath + "/guards/anything":    false,
		"net/http":                         false,
	}
	for path, want := range cases {
		if got := forbiddenImport(path); got != want {
			t.Error(test.DiffMessage(got, want, "B4: forbiddenImport("+path+")"))
		}
	}
}
