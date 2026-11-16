package config

import (
	"maps"
	"slices"

	"github.com/dangduoc08/ginject/internal/inspect"
)

func init() {
	inspect.ConfigStateOf = configStateOf
}

func configStateOf(provider any) (inspect.ConfigState, bool) {
	var st *configState
	switch p := provider.(type) {
	case ConfigService:
		st = p.State
	case *ConfigService:
		if p != nil {
			st = p.State
		}
	}
	if st == nil {
		return inspect.ConfigState{}, false
	}
	return inspect.ConfigState{
		Files: slices.Clone(st.files),
		Keys:  slices.Sorted(maps.Keys(*st.valuesByKey.Load())),
	}, true
}
