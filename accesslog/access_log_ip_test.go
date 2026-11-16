package accesslog

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestAccessLog_IP(t *testing.T) {
	tr, logger, _ := setup(t)
	e := completeEvent("r1", 200, "/users", time.Millisecond)
	e.ClientIP = netip.MustParseAddr("203.0.113.7")
	tr.Emit(e)
	lines := logger.lines()
	if len(lines) != 1 {
		t.Fatal(test.DiffMessage(len(lines), 1, "fixture: one access line"))
	}
	if got := fmt.Sprint(argValue(t, lines[0].args, "ip")); got != "203.0.113.7" {
		t.Error(test.DiffMessage(got, "203.0.113.7", "A1: the line prints the client IP as ip"))
	}
}

func TestAccessLog_IP_IPv6(t *testing.T) {
	tr, logger, _ := setup(t)
	e := completeEvent("r1", 200, "/users", time.Millisecond)
	e.ClientIP = netip.MustParseAddr("2001:db8::1")
	tr.Emit(e)
	if got := fmt.Sprint(argValue(t, logger.lines()[0].args, "ip")); got != "2001:db8::1" {
		t.Error(test.DiffMessage(got, "2001:db8::1", "A1: an IPv6 client prints in its short form"))
	}
}

func TestAccessLog_IPOmittedWhenUnknown(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(completeEvent("r1", 200, "/users", time.Millisecond))
	lines := logger.lines()
	if len(lines) != 1 {
		t.Fatal(test.DiffMessage(len(lines), 1, "fixture: one access line"))
	}
	if hasArg(lines[0].args, "ip") {
		t.Error(test.DiffMessage(argKeys(lines[0].args), "<no ip>", "A1: the zero Addr is omitted"))
	}
}

func TestAccessLog_IP_BuiltinJSON(t *testing.T) {
	tr, out := builtinAccessLog(t, 0)
	e := completeEvent("r1", 200, "/users", time.Millisecond)
	e.ClientIP = netip.MustParseAddr("203.0.113.7")
	tr.Emit(e)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Fatal(test.DiffMessage(out.String(), "one JSON object", "fixture: the built-in JSON line"))
	}
	if parsed["ip"] != "203.0.113.7" {
		t.Error(test.DiffMessage(parsed["ip"], "203.0.113.7", "A1: the built-in logger prints ip as a plain string"))
	}
}

func TestAccessLog_IPOmittedWhenUnknown_BuiltinJSON(t *testing.T) {
	tr, out := builtinAccessLog(t, 0)
	tr.Emit(completeEvent("r1", 200, "/users", time.Millisecond))
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Fatal(test.DiffMessage(out.String(), "one JSON object", "fixture: the built-in JSON line"))
	}
	if _, ok := parsed["ip"]; ok {
		t.Error(test.DiffMessage(parsed, "<no ip>", "A1: the zero Addr is omitted by the built-in logger too"))
	}
}
