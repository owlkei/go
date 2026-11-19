package memorybroker

import (
	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/crypto"
)

func newID() string {
	id, err := crypto.UUID()
	if err != nil {
		panic(color.FmtRed("broker: failed to generate UUID: %v", err))
	}
	return id
}

func forEachPrefixOf(topic string, fn func(prefix string)) {
	for i := len(topic) - 1; i >= 0; i-- {
		if topic[i] == '.' {
			fn(topic[:i])
		}
	}
}
