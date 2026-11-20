package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestConfigService_GetSet(t *testing.T) {
	svc := newService(t)
	if svc.Get("TT_GS_MISSING") != nil {
		t.Error(test.DiffMessage(svc.Get("TT_GS_MISSING"), nil, "E13: Get of a missing key is nil"))
	}
	svc.Set("TT_GS", "one")
	svc.Set("TT_GS_NUM", 42)
	svc.Set("TT_GS_MAP", map[string]any{"a": 1})
	expectValues(t, svc, "E13", map[string]any{"TT_GS": "one", "TT_GS_NUM": 42})
	svc.Set("TT_GS", "two")
	expectValues(t, svc, "E13: Set replaces", map[string]any{"TT_GS": "two"})
	svc.Set("TT_GS", nil)
	expectAbsent(t, svc, "E13: Set to nil", "TT_GS")
}

func TestConfigService_ZeroValueGet(t *testing.T) {
	var svc ConfigService
	if got := svc.Get("anything"); got != nil {
		t.Error(test.DiffMessage(got, nil, "E13: a zero ConfigService{} returns nil"))
	}
}

func TestConfigService_ZeroValueSetPanics(t *testing.T) {
	var svc ConfigService
	text, panicked := panicText(func() { svc.Set("k", "v") })
	want := "invalid config: ConfigService was not created by Register"
	if !panicked || text != want {
		t.Error(test.DiffMessage(text, want, "E13: Set on a zero ConfigService{}"))
	}
}

func TestConfigService_ConcurrentGetSetNoRace(t *testing.T) {
	svc := newService(t)
	svc.Set("TT_RACE_SHARED", "old")
	var wg sync.WaitGroup
	for g := 0; g < 12; g++ {
		wg.Go(func() {
			own := fmt.Sprintf("TT_RACE_%d", g)
			for i := 0; i < 500; i++ {
				svc.Set(own, i)
				svc.Set("TT_RACE_SHARED", "new")
				if got := svc.Get(own); got != i {
					t.Errorf("%v", test.DiffMessage(got, i, "E13: a goroutine reads back its own Set"))
					return
				}
				if v := svc.Get("TT_RACE_SHARED"); v != "old" && v != "new" {
					t.Errorf("%v", test.DiffMessage(v, "old or new", "E13: a Get that runs with a Set sees the old or the new value"))
					return
				}
				svc.Transform(boundConfig{})
			}
		})
	}
	wg.Wait()
	for g := 0; g < 12; g++ {
		if got := svc.Get(fmt.Sprintf("TT_RACE_%d", g)); got != 499 {
			t.Error(test.DiffMessage(got, 499, "E13: no Set is lost"))
		}
	}
}

func TestConfigService_Transform_Strings(t *testing.T) {
	svc := newService(t)
	svc.Set("TT_TR_NAME", "owlkei")
	svc.Set("TT_TR_PORT", "8080")
	dto, _ := svc.Transform(boundConfig{})
	got, ok := dto.(boundConfig)
	if !ok {
		t.Fatal(test.DiffMessage(fmt.Sprintf("%T", dto), "config.boundConfig", "E15: Transform returns the bound struct"))
	}
	if got.Name != "owlkei" || got.Port != 8080 {
		t.Error(test.DiffMessage(fmt.Sprintf("%+v", got), "{Name:owlkei Port:8080 ...}", "E15: string values are bound"))
	}
}

func TestConfigService_Transform_Scalars(t *testing.T) {
	svc := newService(t)
	svc.Set("TT_TR_NAME", "x")
	svc.Set("TT_TR_PORT", 9090)
	svc.Set("TT_TR_BIG", int64(1)<<40)
	svc.Set("TT_TR_SMALL", uint8(7))
	svc.Set("TT_TR_ON", true)
	svc.Set("TT_TR_RATIO", 1.5)
	svc.Set("TT_TR_HALF", float32(0.25))

	var dto any
	text, panicked := panicText(func() { dto, _ = svc.Transform(boundConfig{}) })
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "E15: Transform never panics on a value's type"))
	}
	got, _ := dto.(boundConfig)
	want := boundConfig{Name: "x", Port: 9090, Big: 1 << 40, Small: 7, On: true, Ratio: 1.5, Half: 0.25}
	if got != want {
		t.Error(test.DiffMessage(fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", want), "E15: bool, integer and float values bind from their strconv text"))
	}
}

func TestConfigService_Transform_SkipsMapsAndSlices(t *testing.T) {
	loads := func() map[string]any {
		return map[string]any{"nested": map[string]any{"list": []any{1, 2}}}
	}
	svc := registerWith(t, &ConfigModuleOptions{IsIgnoreEnvFile: true, Loads: []ConfigLoadFn{loads}})
	svc.Set("TT_TR_NAME", "kept")
	svc.Set("TT_TR_PORT", map[string]any{"a": 1})
	svc.Set("TT_TR_ON", []string{"x"})
	svc.Set("TT_TR_RATIO", nil)
	svc.Set("TT_TR_BIG", struct{ X int }{1})

	var dto any
	text, panicked := panicText(func() { dto, _ = svc.Transform(boundConfig{}) })
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "E15: maps, slices and nil are skipped, not a panic"))
	}
	got, _ := dto.(boundConfig)
	if got.Name != "kept" || got.Port != 0 || got.On || got.Ratio != 0 || got.Big != 0 {
		t.Error(test.DiffMessage(fmt.Sprintf("%+v", got), "{Name:kept, the rest zero}", "E15: skipped values bind nothing"))
	}
}

func TestConfigService_NotPrintable(t *testing.T) {
	t.Setenv("TT_NP_ENV_KEY", "ENVSECRET-value")
	svc := registerWith(t, &ConfigModuleOptions{
		IsIgnoreEnvFile: true,
		Loads:           []ConfigLoadFn{func() map[string]any { return map[string]any{"TT_NP_LOADS_KEY": "LOADSSECRET-value"} }},
	})
	svc.Set("TT_NP_KEY", canary)

	forbidden := []string{canary, "ENVSECRET", "LOADSSECRET", "TT_NP_KEY", "TT_NP_ENV_KEY", "TT_NP_LOADS_KEY", "PATH"}
	subjects := map[string]any{
		"service":         svc,
		"service pointer": &svc,
		"holder":          holder{Label: "app", Config: svc},
		"holder pointer":  &holder{Label: "app", Config: svc},
	}

	for name, subject := range subjects {
		outputs := map[string]string{
			"%v":  fmt.Sprintf("%v", subject),
			"%+v": fmt.Sprintf("%+v", subject),
			"%#v": fmt.Sprintf("%#v", subject),
			"%s":  fmt.Sprintf("%s", subject),
		}
		if raw, err := json.Marshal(subject); err == nil {
			outputs["json"] = string(raw)
		}
		var jsonLog, textLog bytes.Buffer
		slog.New(slog.NewJSONHandler(&jsonLog, nil)).Info("config", "value", subject)
		slog.New(slog.NewTextHandler(&textLog, nil)).Info("config", "value", subject)
		outputs["slog json"] = jsonLog.String()
		outputs["slog text"] = textLog.String()

		for format, out := range outputs {
			for _, word := range forbidden {
				if strings.Contains(out, word) {
					t.Error(test.DiffMessage(out, "no "+word, "E10: "+format+" of a "+name+" must contain no key and no value"))
				}
			}
		}
	}
}

func TestConfigService_StateIsOpaque(t *testing.T) {
	svc := newService(t)
	if svc.State == nil {
		t.Error(test.DiffMessage(nil, "a state", "State is set by Register"))
	}
	raw, err := json.Marshal(svc)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"State":{}}` {
		t.Error(test.DiffMessage(string(raw), `{"State":{}}`, "State has no exported field or method"))
	}
}
