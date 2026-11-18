package color

import (
	"testing"
)

func TestColored_CarriesTextAndColor(t *testing.T) {
	c := Colored{Text: "10ms (2%)", Color: Red}
	if c.Text != "10ms (2%)" || c.Color != Red {
		t.Errorf("Q4: Colored holds a text and a color, got %+v", c)
	}
}

func TestColor_ValuesAreTheAnsiCodes(t *testing.T) {
	cases := []struct {
		name string
		got  Color
		want string
	}{
		{"Gray", Gray, "\x1b[90m"},
		{"Green", Green, "\x1b[32m"},
		{"Blue", Blue, "\x1b[34m"},
		{"Yellow", Yellow, "\x1b[33m"},
		{"Orange", Orange, "\x1b[38;5;208m"},
		{"Red", Red, "\x1b[31m"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("Q4: color.%s keeps the code it had in log: got %q, want %q", c.name, string(c.got), c.want)
		}
	}
}
