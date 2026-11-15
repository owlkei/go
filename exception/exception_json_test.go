package exception

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

type jsonAppender interface {
	AppendJSON(dst []byte) []byte
}

func mapBody(e Exception) []byte {
	b, err := json.Marshal(map[string]any{"code": e.GetCode(), "error": e.Error(), "message": e.GetMessage()})
	if err != nil {
		panic(err)
	}
	return b
}

func TestException_AppendJSON(t *testing.T) {
	ex := NewException("bad", http.StatusBadRequest, ExceptionOptions{Description: "validation failed"})
	want := `{"code":400,"error":"validation failed","message":"bad"}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: body bytes"))
	}
}

func TestException_AppendJSON_WithCause(t *testing.T) {
	ex := NewException("bad", http.StatusBadRequest, ExceptionOptions{Description: "d", Cause: errors.New("root")})
	want := `{"code":400,"error":"d: root","message":"bad"}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: error field is Error()"))
	}
}

func TestException_AppendJSON_KeepsPrefix(t *testing.T) {
	ex := NewException("m", http.StatusNotFound)
	got := ex.AppendJSON([]byte("prefix:"))
	want := `prefix:{"code":404,"error":"Not Found","message":"m"}`
	if string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E6: dst content is kept"))
	}
}

func TestException_AppendJSON_ZeroValue(t *testing.T) {
	var ex Exception
	want := `{"code":0,"error":"","message":""}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: zero value is written as is"))
	}
}

func TestException_AppendJSON_WSCode(t *testing.T) {
	ex := NewException("nope", 1008)
	want := `{"code":1008,"error":"Policy Violation","message":"nope"}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: WS code"))
	}
}

func TestException_AppendJSON_HostileStrings(t *testing.T) {
	hostile := []string{
		`"`, `\`, "line\nbreak", "nul\x00byte", "</script>", "a&b", "\u2028\u2029",
		"bad\xff\xfeutf8", "\x7f\x1f", "'single'", strings.Repeat("a\"\\<", 16*1024),
		strings.Repeat("x", 64*1024),
	}
	for _, s := range hostile {
		ex := NewException(s, http.StatusBadRequest, s)
		got := ex.AppendJSON(nil)
		want := mapBody(ex)
		if !bytes.Equal(got, want) {
			t.Error(test.DiffMessage(string(got[:min(len(got), 120)]), string(want[:min(len(want), 120)]), "E6: hostile string must equal json.Marshal"))
		}
		if !json.Valid(got) {
			t.Error(test.DiffMessage(false, true, "E6/Security: hostile body must be valid JSON"))
		}
	}
}

func TestException_AppendJSON_EscapesHTMLAndSeparators(t *testing.T) {
	ex := NewException("</script>&\u2028", http.StatusBadRequest)
	got := string(ex.AppendJSON(nil))
	for _, bad := range []string{"<", ">", "&", "\u2028"} {
		if strings.Contains(got, bad) {
			t.Error(test.DiffMessage(got, "escaped "+bad, "Security: body must escape characters"))
		}
	}
}

func randomString(r *rand.Rand) string {
	pieces := []string{
		"a", "Z", "0", " ", "hello", "Cannot GET /", "\"", "\\", "\n", "\t", "\x00", "\x1f", "<", ">", "&",
		"\u2028", "\u2029", "é", "世界", "😀", "\xff", "\xc3", "\xe2\x80", "'", "/", "\u007f",
	}
	n := r.IntN(12)
	var sb strings.Builder
	for range n {
		sb.WriteString(pieces[r.IntN(len(pieces))])
	}
	return sb.String()
}

func TestException_AppendJSON_EqualsMarshal_Random(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	codes := []int{0, 200, 400, 404, 500, 999, 1008, 1011, -1, 65536}
	for range 2000 {
		code := codes[r.IntN(len(codes))]
		msg, desc := randomString(r), randomString(r)
		var ex Exception
		if r.IntN(2) == 0 {
			ex = NewException(msg, code, ExceptionOptions{Description: desc})
		} else {
			ex = NewException(msg, code, desc)
		}
		got, want := ex.AppendJSON(nil), mapBody(ex)
		if !bytes.Equal(got, want) {
			t.Fatal(test.DiffMessage(string(got), string(want), "E6: random message and description must equal json.Marshal"))
		}
	}
}

func TestException_AppendJSON_Pointer(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest)
	var a jsonAppender = &ex
	var b jsonAppender = ex
	want := string(mapBody(ex))
	if string(a.AppendJSON(nil)) != want || string(b.AppendJSON(nil)) != want {
		t.Error(test.DiffMessage(string(a.AppendJSON(nil)), want, "E6: Exception and *Exception both append the body"))
	}
}

func TestException_MarshalJSON(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest, "d")
	got, err := ex.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":400,"error":"d","message":"m"}`; string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E7: MarshalJSON body"))
	}
	viaJSON, err := json.Marshal(ex)
	if err != nil || string(viaJSON) != string(got) {
		t.Error(test.DiffMessage(string(viaJSON), string(got), "E7: json.Marshal of a value"))
	}
}

func TestException_MarshalJSON_Pointer(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest, "d")
	got, err := json.Marshal(&ex)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":400,"error":"d","message":"m"}`; string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E7: json.Marshal of a pointer"))
	}
}

func TestException_MarshalJSON_InsideMap(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest)
	got, err := json.Marshal(map[string]any{"e": ex, "p": &ex})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"code":400,"error":"Bad Request","message":"m"}`
	if want := `{"e":` + body + `,"p":` + body + `}`; string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E7: exception nested in a map"))
	}
}
