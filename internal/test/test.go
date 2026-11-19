package test

import (
	"strings"

	"go.owlky.com/owlky/internal/color"
)

func DiffMessage(actual, expected any, desc string) string {
	var b strings.Builder
	if desc != "" {
		b.WriteString(color.FmtGreen("\n%v", desc))
	}
	b.WriteString(color.FmtDim("\nExpected: "))
	b.WriteString(color.FmtRed("%v", expected))
	b.WriteString(color.FmtDim("\nActual: "))
	b.WriteString(color.FmtRed("%v", actual))
	return b.String()
}
