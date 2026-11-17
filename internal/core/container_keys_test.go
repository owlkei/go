package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/log"
)

func TestContainer_InterfaceKeysAreReflected(t *testing.T) {
	app := New()
	app.Create(ModuleBuilder().Build())

	got, ok := app.container.interfaceFor(genFieldKey(reflect.TypeFor[log.Logger]()))
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "B21: the app's logger is stored under genFieldKey(log.Logger)"))
	}
	if got != any(app.logger) {
		t.Error(test.DiffMessage(got, app.logger, "B21: the value under the logger key is the app's logger"))
	}
	if _, ok := app.container.interfaceFor(genFieldKey(reflect.TypeFor[common.Publisher]())); !ok {
		t.Error(test.DiffMessage(ok, true, "B21: the app's publisher is stored under genFieldKey(common.Publisher)"))
	}
}

func TestContainer_NoImportPathLiteralKeys(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{"ginject/internal/common/common.", "ginject/common/common.", "ginject/log/log."} {
			if strings.Contains(string(raw), needle) {
				t.Error(test.DiffMessage(file, "no import path literal", "B21: "+file+" holds the literal "+needle))
			}
		}
	}
}
