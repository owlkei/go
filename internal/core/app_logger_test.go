package core

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/log"
)

func TestLogger_DefaultIsNotNil(t *testing.T) {
	app := New()

	if app.Logger() == nil {
		t.Fatal(test.DiffMessage(nil, "a logger", "G1: Logger is never nil before Create"))
	}
	mustCreate(t, app, NewModuleBuilder().Build())
	if app.Logger() == nil {
		t.Fatal(test.DiffMessage(nil, "a logger", "G1: Logger is never nil after Create"))
	}
}

func TestLogger_SameValueEachCall(t *testing.T) {
	app := New()
	first := app.Logger()
	if second := app.Logger(); second != first {
		t.Error(test.DiffMessage(second, first, "G1: Logger returns the same value on every call before Create"))
	}

	mustCreate(t, app, NewModuleBuilder().Build())
	if third := app.Logger(); third != first {
		t.Error(test.DiffMessage(third, first, "G1: Create does not replace the default logger"))
	}
}

func TestSetLogger_LogsReachCustomLogger(t *testing.T) {
	rec := &createLogger{}
	app := New()

	if got := app.SetLogger(rec); got != app {
		t.Error(test.DiffMessage(got, app, "G2: SetLogger returns the app"))
	}
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Logger().Info("hello", "k", "v")

	if got := len(rec.named("hello")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "G2: what is logged through app.Logger() reaches the custom logger"))
	}
}

func TestSetLogger_InjectedFieldsUseIt(t *testing.T) {
	rec := &createLogger{}
	app := New()
	app.SetLogger(rec)
	mustCreate(t, app, NewModuleBuilder().Providers(isoLogProvider{}).Build())

	if got := len(rec.named("provider-ready")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "G2: a log.Logger field is injected with the app's logger"))
	}
}

func TestSetLogger_CustomLoggerGetsTagsAndMasking(t *testing.T) {
	type secret struct {
		Value string `log:"value"`
		skip  string
	}

	rec := &createLogger{}
	app := New()
	app.SetLogger(rec)
	mustCreate(t, app, NewModuleBuilder().Build())

	app.Logger().Info("tagged", "s", secret{Value: "visible", skip: "hidden"})

	calls := rec.named("tagged")
	if len(calls) != 1 {
		t.Fatal(test.DiffMessage(len(calls), 1, "G3: the custom logger is called"))
	}
	got, ok := calls[0].args[1].(map[string]any)
	if !ok {
		t.Fatalf("G3: expected the struct to be expanded into a map[string]any, got %T", calls[0].args[1])
	}
	if got["value"] != "visible" {
		t.Error(test.DiffMessage(got["value"], "visible", "G3: a tagged field is logged under its tag"))
	}
	if _, exists := got["skip"]; exists {
		t.Error(test.DiffMessage(true, false, "G3: an untagged unexported field is not logged"))
	}
}

func TestSetLogger_WrappedLoggerGetsMasking(t *testing.T) {
	rec := &createLogger{}
	app := New()
	app.SetLogger(log.WrapLogger(rec, []string{"password"}))
	mustCreate(t, app, NewModuleBuilder().Build())

	app.Logger().Info("masked", "password", "secret")

	calls := rec.named("masked")
	if len(calls) != 1 || calls[0].args[1] != "[REDACTED]" {
		t.Error(test.DiffMessage(calls, "one call with [REDACTED]", "G6: the masking fields of log.WrapLogger apply"))
	}
}

func TestSetLogger_WrappedLoggerNotWrappedAgain(t *testing.T) {
	rec := &createLogger{}
	wrapped := log.WrapLogger(rec, nil)
	app := New()
	app.SetLogger(wrapped)
	mustCreate(t, app, NewModuleBuilder().Build())

	if got := app.Logger(); got != wrapped {
		t.Error(test.DiffMessage(got, wrapped, "G3: a logger from log.WrapLogger is used as it is"))
	}

	built := log.NewLog(nil)
	other := New()
	other.SetLogger(built)
	mustCreate(t, other, NewModuleBuilder().Build())
	if got := other.Logger(); got != built {
		t.Error(test.DiffMessage(got, built, "G3: a logger from log.NewLog is used as it is"))
	}
}

func TestSetLogger_NilPanics(t *testing.T) {
	msExpectPanic(t, "G4", "invalid app: SetLogger needs a logger", func() {
		New().SetLogger(nil)
	})
}

func TestSetLogger_AfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: SetLogger must be called before Create", func() {
		app.SetLogger(&createLogger{})
	})
}
