package log

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

type groupValuer struct{}

func (groupValuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("name", "duoc"), slog.String("password", "s3cret-group"))
}

func TestLog_MasksKeyAfterNonStringKey(t *testing.T) {
	for _, format := range allFormats {
		out := logOnce(format, LogOptions{MaskFields: []string{"password"}}, "login failed", errors.New("bad credentials"), "password", "s3cret-pw")
		if strings.Contains(out, "s3cret-pw") {
			t.Error(test.DiffMessage(out, "<no password>", "M3: password is masked after a non-string key ("+formatName(format)+")"))
		}
	}
}

func TestLog_MasksKeyAfterSeveralNonStringKeys(t *testing.T) {
	for _, format := range allFormats {
		out := logOnce(format, LogOptions{MaskFields: []string{"password"}}, "m", 1, 2, 3, "password", "s3cret-pw", "ok", "visible")
		if strings.Contains(out, "s3cret-pw") {
			t.Error(test.DiffMessage(out, "<no password>", "M3: password is masked after three non-string keys ("+formatName(format)+")"))
		}
		if !strings.Contains(out, "visible") {
			t.Error(test.DiffMessage(out, "contains visible", "M3: unrelated pairs stay ("+formatName(format)+")"))
		}
	}
}

func TestLog_MasksKeyAfterNonStringKeyBeforeAttr(t *testing.T) {
	for _, format := range allFormats {
		out := logOnce(format, LogOptions{MaskFields: []string{"password"}}, "m", errors.New("e"), slog.String("password", "s3cret-attr"))
		if strings.Contains(out, "s3cret-attr") {
			t.Error(test.DiffMessage(out, "<no password>", "M3: an Attr after a non-string key is masked ("+formatName(format)+")"))
		}
	}
}

func TestWrapLogger_MasksKeyAfterNonStringKey(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"password"}).Error("login failed", errors.New("bad credentials"), "password", "s3cret-pw")
	if strings.Contains(fmt.Sprint(cap.args()...), "s3cret-pw") {
		t.Error(test.DiffMessage(cap.args(), "<no password>", "M3: WrapLogger masks password after a non-string key"))
	}
}

func TestLog_LogValuerGroupMaskedByPath(t *testing.T) {
	for _, rule := range []string{"password", "user.password"} {
		for _, format := range allFormats {
			out := logOnce(format, LogOptions{MaskFields: []string{rule}}, "m", "user", groupValuer{})
			if strings.Contains(out, "s3cret-group") {
				t.Error(test.DiffMessage(out, "<no password>", "L1: a LogValuer group is masked by path with rule "+rule+" ("+formatName(format)+")"))
			}
			if !strings.Contains(out, "duoc") {
				t.Error(test.DiffMessage(out, "contains duoc", "L1: siblings in the group stay ("+formatName(format)+")"))
			}
		}
	}
}

func TestWrapLogger_LogValuerGroupMaskedByPath(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"password"}).Info("m", "user", groupValuer{})
	if strings.Contains(fmt.Sprint(cap.args()...), "s3cret-group") {
		t.Error(test.DiffMessage(cap.args(), "<no password>", "L1: WrapLogger masks a LogValuer group"))
	}
}
