package log

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestFrom_AddSourceIsCaller(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{IsAddSource: true})
	_, _, line, _ := runtime.Caller(0)
	l.From(requestContext()).Info("m", "a", 1)
	var out struct {
		Source struct {
			Function string `json:"function"`
			File     string `json:"file"`
			Line     int    `json:"line"`
		} `json:"source"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.Source.File, "log_from_test.go") || !strings.Contains(out.Source.Function, "TestFrom_AddSourceIsCaller") || out.Source.Line != line+1 {
		t.Error(test.DiffMessage(out.Source, fmt.Sprintf("log_from_test.go:%d in TestFrom_AddSourceIsCaller", line+1), "F8: the source is the caller of Info, as without From"))
	}
}
