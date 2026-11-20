package config

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strconv"

	"go.owlkei.com/owlkei/internal/color"
)

const maxLoadsDepth = 32

func failConfig(detail string) {
	panic(color.FmtRed("invalid config: " + detail))
}

func isValidKey(k string) bool {
	if len(k) == 0 {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		isLetter := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !isLetter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func flatten(org, children any, prefix string) any {
	nested := org
	if prefix != "" {
		nested = children
	}
	out := map[string]any{}
	if !flattenInto(out, nested, prefix, 1) {
		return nil
	}
	m, _ := org.(map[string]any)
	maps.Copy(m, out)
	return m
}

func flattenInto(out map[string]any, nested any, prefix string, depth int) bool {
	if depth > maxLoadsDepth {
		return false
	}
	switch nested := nested.(type) {
	case []any:
		for index, value := range nested {
			if !flattenValue(out, value, strconv.Itoa(index), prefix, depth) {
				return false
			}
		}
	case map[string]any:
		for key, value := range nested {
			if !flattenValue(out, value, key, prefix, depth) {
				return false
			}
		}
	}
	return true
}

func flattenValue(out map[string]any, value any, key, prefix string, depth int) bool {
	name := key
	if prefix != "" {
		name = prefix + "." + key
		out[name] = value
	}
	switch value.(type) {
	case map[string]any, []any:
		return flattenInto(out, value, name, depth+1)
	}
	return true
}

func findRootDir(anchor string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, anchor)); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("root not found: " + anchor)
		}
		dir = parent
	}
}
