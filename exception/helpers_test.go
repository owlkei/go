package exception

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
)

type jsonAppender interface {
	AppendJSON(dst []byte) []byte
}

func mapBody(e Exception) []byte {
	b, err := json.Marshal(map[string]any{"code": e.Code(), "error": e.Error(), "message": e.Message()})
	if err != nil {
		panic(err)
	}
	return b
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

type bodyForEncodingJSON struct {
	Code    int    `json:"code"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

func encodingJSONBody(t testing.TB, e Exception) []byte {
	t.Helper()
	b, err := json.Marshal(bodyForEncodingJSON{Code: e.Code(), Error: e.Error(), Message: e.Message()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type causeBox struct{ error }

var sinkException Exception
