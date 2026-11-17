package config

import (
	"maps"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/ctx"
)

type ConfigService struct {
	State *configState
}

type configState struct {
	mu          sync.Mutex
	valuesByKey atomic.Pointer[map[string]any]
	files       []string
}

func newConfigState(files []string, valuesByKey map[string]any) *configState {
	st := &configState{files: slices.Clone(files)}
	st.valuesByKey.Store(&valuesByKey)
	return st
}

func (cs ConfigService) NewProvider() core.Provider {
	return cs
}

func (cs *ConfigService) Get(k string) any {
	if cs.State == nil {
		return nil
	}
	return (*cs.State.valuesByKey.Load())[k]
}

func (cs *ConfigService) Set(k string, v any) {
	if cs.State == nil {
		failConfig("ConfigService was not created by Register")
	}
	st := cs.State
	st.mu.Lock()
	next := maps.Clone(*st.valuesByKey.Load())
	next[k] = v
	st.valuesByKey.Store(&next)
	st.mu.Unlock()
}

func (cs *ConfigService) Transform(s any) (any, []ctx.FieldLevel) {
	conf := map[string][]string{}
	if cs.State != nil {
		for key, val := range *cs.State.valuesByKey.Load() {
			if text, ok := scalarText(val); ok {
				conf[key] = []string{text}
			}
		}
	}
	return ctx.BindStrArr(conf, &[]ctx.FieldLevel{}, s)
}

func scalarText(val any) (string, bool) {
	switch v := val.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	}
	return "", false
}
