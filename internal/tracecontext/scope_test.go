package tracecontext

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/test"
)

func TestNewRequest_OwnTrace(t *testing.T) {
	c, s := NewRequest(context.Background(), http.Header{}, fixedNow)
	if len(s.TraceID) != 32 || !isLowerHex(s.TraceID) {
		t.Error(test.DiffMessage(s.TraceID, "<32 hex characters>", "S3: a request without traceparent starts its own trace"))
	}
	if want := fmt.Sprintf("%012x", fixedNow.UnixMilli()); s.TraceID[:12] != want {
		t.Error(test.DiffMessage(s.TraceID[:12], want, "S3: the own trace ID is made from the given time"))
	}
	if len(s.Request) != 16 || !isLowerHex(s.Request) {
		t.Error(test.DiffMessage(s.Request, "<16 hex characters>", "S2: Request is a span ID"))
	}
	if s.Parent != "" || s.Upstream != "" || s.Connection != "" || s.Message != "" {
		t.Error(test.DiffMessage([]string{s.Parent, s.Upstream, s.Connection, s.Message}, []string{"", "", "", ""}, "S3: Parent, Upstream, Connection and Message are empty"))
	}
	if ScopeOf(c) != s {
		t.Error(test.DiffMessage(ScopeOf(c), s, "S1: the returned context carries the returned scope"))
	}
	if s.LogSlot.Load() != nil {
		t.Error(test.DiffMessage(s.LogSlot.Load(), nil, "Coordination: LogSlot is left empty for design 0012"))
	}
}

func TestNewRequest_NilHeader(t *testing.T) {
	_, s := NewRequest(context.Background(), nil, fixedNow)
	if s == nil || len(s.TraceID) != 32 || len(s.Request) != 16 {
		t.Error(test.DiffMessage(s, "<a scope with its own IDs>", "S3: a nil header is a request without headers"))
	}
}

func TestNewRequest_ContinuesTrace(t *testing.T) {
	c, s := NewRequest(context.Background(), headerOf("Traceparent", validHeader), fixedNow)
	if s.TraceID != validTraceID {
		t.Error(test.DiffMessage(s.TraceID, validTraceID, "S3: a valid traceparent continues its trace ID"))
	}
	if s.Parent != validParentID {
		t.Error(test.DiffMessage(s.Parent, validParentID, "S3: Parent is the incoming parent ID"))
	}
	if s.Request == validParentID {
		t.Error(test.DiffMessage(s.Request, "<a fresh ID>", "S2: the request is not the incoming parent"))
	}
	if ScopeOf(c) != s {
		t.Error(test.DiffMessage(ScopeOf(c), s, "S1: the context carries the scope"))
	}
}

func TestNewRequest_InvalidTraceparentIgnored(t *testing.T) {
	for _, bad := range []string{
		"",
		"garbage",
		strings.ToUpper(validHeader),
		validHeader + "-extra",
		"00-" + strings.Repeat("0", 32) + "-" + validParentID + "-01",
		"ff-" + validTraceID + "-" + validParentID + "-01",
		validHeader + "\r\nX-Injected: 1",
		strings.Repeat("a", 1<<16),
	} {
		_, s := NewRequest(context.Background(), headerOf("Traceparent", bad), fixedNow)
		if s.TraceID == validTraceID || s.Parent != "" {
			t.Error(test.DiffMessage([]string{s.TraceID, s.Parent}, []string{"<own trace>", ""}, "S3: an invalid traceparent is ignored"))
		}
		if want := fmt.Sprintf("%012x", fixedNow.UnixMilli()); s.TraceID[:12] != want {
			t.Error(test.DiffMessage(s.TraceID[:12], want, "S3: an ignored traceparent leaves an own, time-ordered trace ID"))
		}
		if s.Upstream != "" {
			t.Error(test.DiffMessage(s.Upstream, "", "S4: an invalid traceparent gives no upstream"))
		}
	}
}

func TestNewRequest_TwoTraceparentValuesIgnored(t *testing.T) {
	other := "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01"
	_, s := NewRequest(context.Background(), headerOf("Traceparent", validHeader, "Traceparent", other), fixedNow)
	if s.TraceID == validTraceID || s.TraceID == strings.Repeat("a", 32) || s.Parent != "" || s.Upstream != "" {
		t.Error(test.DiffMessage([]string{s.TraceID, s.Parent, s.Upstream}, []string{"<own trace>", "", ""}, "S3: two traceparent values are refused without being parsed"))
	}
}

func TestNewRequest_RequestIDNeverFromHeader(t *testing.T) {
	for _, incoming := range []string{"abcdef0123456789", "client-chosen", validParentID, ""} {
		_, s := NewRequest(context.Background(), headerOf("X-Request-Id", incoming, "Traceparent", validHeader), fixedNow)
		if s.Request == incoming || s.Request == validParentID {
			t.Error(test.DiffMessage(s.Request, "<fresh, not the header value>", "S2: no header sets the request ID"))
		}
	}
	seen := map[string]struct{}{}
	for range 100 {
		_, s := NewRequest(context.Background(), headerOf("X-Request-Id", "same"), fixedNow)
		seen[s.Request] = struct{}{}
	}
	if len(seen) != 100 {
		t.Error(test.DiffMessage(len(seen), 100, "S2: every request gets a fresh ID"))
	}
}

func TestNewRequest_UpstreamFromTraceparent(t *testing.T) {
	_, s := NewRequest(context.Background(), headerOf("Traceparent", validHeader, "X-Request-Id", "other-id"), fixedNow)
	if s.Upstream != validParentID {
		t.Error(test.DiffMessage(s.Upstream, validParentID, "S4: the traceparent parent ID wins over X-Request-Id"))
	}
}

func TestNewRequest_UpstreamFromRequestID(t *testing.T) {
	_, s := NewRequest(context.Background(), headerOf("X-Request-Id", "req-123_a.B"), fixedNow)
	if s.Upstream != "req-123_a.B" {
		t.Error(test.DiffMessage(s.Upstream, "req-123_a.B", "S4: a valid X-Request-Id is the upstream"))
	}
	_, s = NewRequest(context.Background(), headerOf("X-Request-Id", "req-9", "Traceparent", "bad"), fixedNow)
	if s.Upstream != "req-9" {
		t.Error(test.DiffMessage(s.Upstream, "req-9", "S4: an invalid traceparent falls back to X-Request-Id"))
	}
}

func TestNewRequest_UpstreamRejectsBadCharacters(t *testing.T) {
	for _, bad := range []string{
		"has space", "tab\there", "line\nbreak", "cr\rlf", "quote\"", "single'", "slash/", "colon:", "semi;",
		"unicode-é", "nul\x00", "esc\x1b[2K", "<script>", "a=b", "comma,", "",
	} {
		_, s := NewRequest(context.Background(), headerOf("X-Request-Id", bad), fixedNow)
		if s.Upstream != "" {
			t.Error(test.DiffMessage(s.Upstream, "", fmt.Sprintf("S4: X-Request-Id %q is refused", bad)))
		}
	}
}

func TestNewRequest_UpstreamLengthBoundaries(t *testing.T) {
	_, s := NewRequest(context.Background(), headerOf("X-Request-Id", strings.Repeat("a", 64)), fixedNow)
	if s.Upstream != strings.Repeat("a", 64) {
		t.Error(test.DiffMessage(s.Upstream, strings.Repeat("a", 64), "S4: 64 bytes are accepted"))
	}
	_, s = NewRequest(context.Background(), headerOf("X-Request-Id", "a"), fixedNow)
	if s.Upstream != "a" {
		t.Error(test.DiffMessage(s.Upstream, "a", "S4: one byte is accepted"))
	}
}

func TestNewRequest_UpstreamRejects65Bytes(t *testing.T) {
	_, s := NewRequest(context.Background(), headerOf("X-Request-Id", strings.Repeat("a", 65)), fixedNow)
	if s.Upstream != "" {
		t.Error(test.DiffMessage(s.Upstream, "", "S4: 65 bytes are refused"))
	}
	_, s = NewRequest(context.Background(), headerOf("X-Request-Id", strings.Repeat("a", 1<<20)), fixedNow)
	if s.Upstream != "" {
		t.Error(test.DiffMessage(len(s.Upstream), 0, "S4: a megabyte is refused"))
	}
}

func TestNewRequest_UpstreamTwoRequestIDsRefused(t *testing.T) {
	_, s := NewRequest(context.Background(), headerOf("X-Request-Id", "one", "X-Request-Id", "two"), fixedNow)
	if s.Upstream != "" {
		t.Error(test.DiffMessage(s.Upstream, "", "S4: X-Request-Id must have exactly one value"))
	}
}

func TestNewRequest_FlagsContinued(t *testing.T) {
	for _, tc := range []struct {
		incoming string
		want     byte
	}{{"00", 0x00}, {"01", 0x01}, {"02", 0x02}, {"03", 0x03}} {
		_, s := NewRequest(context.Background(), headerOf("Traceparent", "00-"+validTraceID+"-"+validParentID+"-"+tc.incoming), fixedNow)
		if got := flagsOf(t, s); got != tc.want {
			t.Error(test.DiffMessage(got, tc.want, "S5: incoming flags "+tc.incoming+" are kept"))
		}
	}
}

func TestNewRequest_FlagsUnknownBitsCleared(t *testing.T) {
	for _, tc := range []struct {
		incoming string
		want     byte
	}{{"ff", 0x03}, {"fd", 0x01}, {"80", 0x00}, {"a6", 0x02}, {"fc", 0x00}} {
		_, s := NewRequest(context.Background(), headerOf("Traceparent", "00-"+validTraceID+"-"+validParentID+"-"+tc.incoming), fixedNow)
		if got := flagsOf(t, s); got != tc.want {
			t.Error(test.DiffMessage(got, tc.want, "S5: incoming flags "+tc.incoming+" & 0x03"))
		}
	}
}

func TestNewRequest_FlagsOwn(t *testing.T) {
	_, s := NewRequest(context.Background(), http.Header{}, fixedNow)
	if got := flagsOf(t, s); got != 0x03 {
		t.Error(test.DiffMessage(got, byte(0x03), "S5, Q1: a trace we start is sampled and random"))
	}
	_, s = NewRequest(context.Background(), headerOf("Traceparent", "bad"), fixedNow)
	if got := flagsOf(t, s); got != 0x03 {
		t.Error(test.DiffMessage(got, byte(0x03), "S5, Q1: an ignored traceparent starts an own trace with 03"))
	}
}

func TestNewRequest_Allocations(t *testing.T) {
	h := http.Header{}
	if got := testing.AllocsPerRun(200, func() { NewRequest(context.Background(), h, fixedNow) }); got > 2 {
		t.Error(test.DiffMessage(got, "<= 2", "Performance: the traceparent string and the scope context"))
	}
	h = headerOf("Traceparent", validHeader, "X-Request-Id", "abc")
	if got := testing.AllocsPerRun(200, func() { NewRequest(context.Background(), h, fixedNow) }); got > 2 {
		t.Error(test.DiffMessage(got, "<= 2", "Performance: continuing a trace allocates no more"))
	}
}

func TestScope_Traceparent(t *testing.T) {
	_, s := NewRequest(context.Background(), headerOf("Traceparent", validHeader), fixedNow)
	value := s.Traceparent()
	if len(value) != 55 {
		t.Error(test.DiffMessage(len(value), 55, "S6: the traceparent is 55 bytes"))
	}
	traceID, parentID, flags, ok := ParseTraceparent(value)
	if !ok || traceID != s.TraceID || parentID != s.Request || flags != 1 {
		t.Error(test.DiffMessage([]any{traceID, parentID, flags, ok}, []any{s.TraceID, s.Request, byte(1), true}, "S6: it parses back to the trace, the request as span, the flags"))
	}
	if !strings.HasPrefix(value, "00-") {
		t.Error(test.DiffMessage(value[:3], "00-", "S6: version 00"))
	}
	if got := testing.AllocsPerRun(100, func() { _ = s.Traceparent() }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "Performance: the request scope holds its traceparent"))
	}
}

func TestScope_SpanID(t *testing.T) {
	_, request := NewRequest(context.Background(), http.Header{}, fixedNow)
	if request.SpanID() != request.Request {
		t.Error(test.DiffMessage(request.SpanID(), request.Request, "S6: SpanID is Request on an HTTP scope"))
	}
	_, message := NewMessage(context.Background(), fixedNow)
	if message.SpanID() != message.Message || message.SpanID() == "" {
		t.Error(test.DiffMessage(message.SpanID(), message.Message, "S6: SpanID is Message when Request is empty"))
	}
}

func TestScopeOf_ThroughWrappers(t *testing.T) {
	type key struct{}
	c, s := NewRequest(context.Background(), http.Header{}, fixedNow)
	wrapped := context.WithValue(c, key{}, 1)
	wrapped, cancel := context.WithCancel(wrapped)
	defer cancel()
	wrapped, cancelTimeout := context.WithTimeout(wrapped, time.Hour)
	defer cancelTimeout()
	wrapped = context.WithValue(wrapped, key{}, 2)
	if ScopeOf(wrapped) != s {
		t.Error(test.DiffMessage(ScopeOf(wrapped), s, "S9: the scope is found through WithValue, WithCancel and WithTimeout"))
	}
	if got := testing.AllocsPerRun(100, func() { _ = ScopeOf(wrapped) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "S9: ScopeOf allocates nothing"))
	}
}

func TestScopeOf_Nil(t *testing.T) {
	var c context.Context
	if got := ScopeOf(c); got != nil {
		t.Error(test.DiffMessage(got, nil, "S9: ScopeOf(nil) is nil"))
	}
}

func TestScopeOf_Background(t *testing.T) {
	if got := ScopeOf(context.Background()); got != nil {
		t.Error(test.DiffMessage(got, nil, "S9: a context from outside a request has no scope"))
	}
	if got := ScopeOf(context.WithValue(context.TODO(), unrelatedKey{}, 1)); got != nil {
		t.Error(test.DiffMessage(got, nil, "S9: other values are not a scope"))
	}
}

func TestScopeOf_NearestScopeWins(t *testing.T) {
	outer, outerScope := NewRequest(context.Background(), http.Header{}, fixedNow)
	inner, innerScope := NewRequest(outer, http.Header{}, fixedNow)
	if ScopeOf(inner) != innerScope || ScopeOf(outer) != outerScope || innerScope == outerScope {
		t.Error(test.DiffMessage(ScopeOf(inner), innerScope, "S11: the nearest scope is the one returned"))
	}
	if innerScope.TraceID == outerScope.TraceID {
		t.Error(test.DiffMessage(innerScope.TraceID, "<its own trace>", "S11: a context that carries a scope does not change how the new one is built"))
	}
}

func TestWith_AttachesScope(t *testing.T) {
	s := &Scope{TraceID: validTraceID, Request: validParentID}
	c := With(context.Background(), s)
	if ScopeOf(c) != s {
		t.Error(test.DiffMessage(ScopeOf(c), s, "With attaches the scope"))
	}
}

func TestScopeContext_ForwardsDone(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	c, _ := NewRequest(parent, http.Header{}, fixedNow)
	if c.Err() != nil {
		t.Error(test.DiffMessage(c.Err(), nil, "S9: Err is the parent's"))
	}
	select {
	case <-c.Done():
		t.Fatal("S9: Done closed before the parent was cancelled")
	default:
	}
	cancel()
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("S9: Done did not follow the parent")
	}
	if c.Err() != context.Canceled {
		t.Error(test.DiffMessage(c.Err(), context.Canceled, "S9: Err follows the parent"))
	}
}

func TestScopeContext_ForwardsDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	c, _ := NewRequest(parent, http.Header{}, fixedNow)
	got, ok := c.Deadline()
	if !ok || !got.Equal(deadline) {
		t.Error(test.DiffMessage([]any{got, ok}, []any{deadline, true}, "S9: Deadline is the parent's"))
	}
}

func TestScopeContext_KeepsParentValues(t *testing.T) {
	type key struct{}
	parent := context.WithValue(context.Background(), key{}, "v")
	c, _ := NewRequest(parent, http.Header{}, fixedNow)
	if c.Value(key{}) != "v" {
		t.Error(test.DiffMessage(c.Value(key{}), "v", "S1: values of the parent context stay readable"))
	}
}

func TestScopeContext_WithCancelStartsNoGoroutine(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := NewRequest(parent, http.Header{}, fixedNow)
	before := runtime.NumGoroutine()
	cancels := make([]context.CancelFunc, 0, 100)
	for range 100 {
		_, cancelChild := context.WithCancel(c)
		cancels = append(cancels, cancelChild)
	}
	after := runtime.NumGoroutine()
	for _, cancelChild := range cancels {
		cancelChild()
	}
	if after != before {
		t.Error(test.DiffMessage(after, before, "Concurrency: WithCancel on the scope context starts no goroutine"))
	}
}

func TestScope_ConcurrentReadsNoRace(t *testing.T) {
	c, s := NewRequest(context.Background(), headerOf("Traceparent", validHeader, "X-Request-Id", "abc"), fixedNow)
	want := s.Traceparent()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				got := ScopeOf(c)
				if got != s || got.Traceparent() != want || len(got.AppendArgs(nil)) != 6 {
					t.Error(test.DiffMessage(got, s, "Concurrency: a scope is read-only after it is published"))
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestNewMessage_FromConnection(t *testing.T) {
	connCtx, conn := connectionContext(t, "01")
	c, s := NewMessage(connCtx, fixedNow)
	if s.TraceID != conn.TraceID {
		t.Error(test.DiffMessage(s.TraceID, conn.TraceID, "W4: the message continues the connection's trace"))
	}
	if s.Connection != conn.Request || s.Parent != conn.Request {
		t.Error(test.DiffMessage([]string{s.Connection, s.Parent}, []string{conn.Request, conn.Request}, "W4: Connection and Parent are the connection ID"))
	}
	if len(s.Message) != 16 || !isLowerHex(s.Message) || s.Message == conn.Request {
		t.Error(test.DiffMessage(s.Message, "<a fresh span ID>", "W4: Message is a fresh span ID"))
	}
	if s.Request != "" || s.Upstream != "" {
		t.Error(test.DiffMessage([]string{s.Request, s.Upstream}, []string{"", ""}, "W4: Request and Upstream are empty"))
	}
	if flagsOf(t, s) != 0x01 {
		t.Error(test.DiffMessage(flagsOf(t, s), byte(1), "W4: the flags are the connection's"))
	}
	if _, parentID, _, ok := ParseTraceparent(s.Traceparent()); !ok || parentID != s.Message {
		t.Error(test.DiffMessage(parentID, s.Message, "S6: the message is the span of its traceparent"))
	}
	if ScopeOf(c) != s {
		t.Error(test.DiffMessage(ScopeOf(c), s, "W4: the returned context carries the message scope"))
	}
}

func TestNewMessage_ForwardsConnectionCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	connCtx, _ := NewRequest(parent, http.Header{}, fixedNow)
	c, _ := NewMessage(connCtx, fixedNow)
	cancel()
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("W5: the message context is done when the connection's context ends")
	}
}

func TestNewMessage_WithoutConnection(t *testing.T) {
	c, s := NewMessage(context.Background(), fixedNow)
	if len(s.TraceID) != 32 || s.TraceID[:12] != fmt.Sprintf("%012x", fixedNow.UnixMilli()) {
		t.Error(test.DiffMessage(s.TraceID, "<a new trace ID made from now>", "W4: without a connection scope the message starts a trace"))
	}
	if s.Connection != "" || s.Parent != "" || s.Request != "" || s.Upstream != "" {
		t.Error(test.DiffMessage([]string{s.Connection, s.Parent, s.Request, s.Upstream}, []string{"", "", "", ""}, "W4: Connection and Parent are empty"))
	}
	if len(s.Message) != 16 || !isLowerHex(s.Message) {
		t.Error(test.DiffMessage(s.Message, "<16 hex characters>", "W4: the message has its own ID"))
	}
	if flagsOf(t, s) != 0x03 {
		t.Error(test.DiffMessage(flagsOf(t, s), byte(3), "S5: an own trace carries 03"))
	}
	if ScopeOf(c) != s {
		t.Error(test.DiffMessage(ScopeOf(c), s, "W4: the context carries the scope"))
	}
}

func TestNewMessage_Allocations(t *testing.T) {
	connCtx, _ := connectionContext(t, "03")
	if got := testing.AllocsPerRun(200, func() { NewMessage(connCtx, fixedNow) }); got > 3 {
		t.Error(test.DiffMessage(got, "<= 3", "Performance: a message costs the scope context, the ID and its traceparent"))
	}
}

func TestScope_AppendArgs_HTTP(t *testing.T) {
	_, s := NewRequest(context.Background(), http.Header{}, fixedNow)
	got := s.AppendArgs(nil)
	want := []any{KeyTraceID, s.TraceID, KeyRequest, s.Request}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: an HTTP scope without upstream yields 4 elements"))
	}
}

func TestScope_AppendArgs_HTTPWithUpstream(t *testing.T) {
	_, s := NewRequest(context.Background(), headerOf("Traceparent", validHeader), fixedNow)
	got := s.AppendArgs(nil)
	want := []any{KeyTraceID, validTraceID, KeyRequest, s.Request, KeyUpstream, validParentID}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: an HTTP scope with upstream yields 6 elements, in order"))
	}
}

func TestScope_AppendArgs_Message(t *testing.T) {
	connCtx, conn := connectionContext(t, "03")
	_, s := NewMessage(connCtx, fixedNow)
	got := s.AppendArgs(nil)
	want := []any{KeyTraceID, conn.TraceID, KeyConnection, conn.Request, KeyMessage, s.Message}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: a message scope yields trace_id, connection, message"))
	}
	_, alone := NewMessage(context.Background(), fixedNow)
	got = alone.AppendArgs(nil)
	want = []any{KeyTraceID, alone.TraceID, KeyMessage, alone.Message}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: empty values are skipped"))
	}
}

func TestScope_AppendArgs_NilScope(t *testing.T) {
	var s *Scope
	dst := []any{"a", 1}
	got := s.AppendArgs(dst)
	if !slices.Equal(got, dst) {
		t.Error(test.DiffMessage(got, dst, "L1: a nil scope returns dst"))
	}
	if got := s.AppendArgs(nil); got != nil {
		t.Error(test.DiffMessage(got, nil, "L1: a nil scope and a nil dst return nil"))
	}
}

func TestScope_AppendArgs_KeepsExisting(t *testing.T) {
	s := &Scope{TraceID: validTraceID, Request: validParentID}
	got := s.AppendArgs([]any{"user", "ann"})
	want := []any{"user", "ann", KeyTraceID, validTraceID, KeyRequest, validParentID}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: existing arguments stay in front"))
	}
}

func TestScope_AppendArgs_FieldNames(t *testing.T) {
	for got, want := range map[string]string{
		KeyTraceID: "trace_id", KeyRequest: "request", KeyConnection: "connection", KeyMessage: "message", KeyUpstream: "upstream",
	} {
		if got != want {
			t.Error(test.DiffMessage(got, want, "L1: the field names are final"))
		}
	}
}

func TestScope_AppendArgs_SkipsEveryEmptyValue(t *testing.T) {
	s := &Scope{Upstream: "u"}
	got := s.AppendArgs(nil)
	want := []any{KeyUpstream, "u"}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: only non-empty values are appended"))
	}
}

func TestNewRequest_OneReadOf18Bytes(t *testing.T) {
	installReader(t, &streamReader{})
	resetSources()

	starts := make([]byte, 0, 100)
	for i := range 100 {
		_, s := NewRequest(t.Context(), nil, fixedNow)
		raw := mustDecode(t, s.TraceID[12:]+s.Request)
		if !isConsecutive(raw) {
			t.Fatal(test.DiffMessage(hex.EncodeToString(raw), "<18 consecutive stream values>", fmt.Sprintf("R3: request %d: the request ID's bytes directly follow the trace tail's", i)))
		}
		starts = append(starts, raw[0])
	}
	if !hasDelta(starts, 18) {
		t.Error(test.DiffMessage(starts, "<consecutive requests 18 stream bytes apart>", "R3: a request consumes 18 bytes"))
	}
}

func TestNewRequest_ContinuedTraceReads8Bytes(t *testing.T) {
	installReader(t, &streamReader{})
	resetSources()

	h := headerOf("Traceparent", validHeader)
	starts := make([]byte, 0, 100)
	for i := range 100 {
		_, s := NewRequest(t.Context(), h, fixedNow)
		if s.TraceID != validTraceID {
			t.Fatal(test.DiffMessage(s.TraceID, validTraceID, "R3: the trace ID is the header's"))
		}
		raw := mustDecode(t, s.Request)
		if !isConsecutive(raw) {
			t.Fatal(test.DiffMessage(hex.EncodeToString(raw), "<8 consecutive stream values>", fmt.Sprintf("R3: request %d: the request ID is one run of the stream", i)))
		}
		starts = append(starts, raw[0])
	}
	if !hasDelta(starts, 8) {
		t.Error(test.DiffMessage(starts, "<consecutive requests 8 stream bytes apart>", "R3: a continued trace consumes 8 bytes"))
	}
}

func TestNewMessage_UnderConnectionReads8Bytes(t *testing.T) {
	installReader(t, &streamReader{})
	resetSources()

	connCtx, _ := connectionContext(t, "01")
	starts := make([]byte, 0, 100)
	for i := range 100 {
		_, s := NewMessage(connCtx, fixedNow)
		raw := mustDecode(t, s.Message)
		if !isConsecutive(raw) {
			t.Fatal(test.DiffMessage(hex.EncodeToString(raw), "<8 consecutive stream values>", fmt.Sprintf("R3: message %d: the ID is one run of the stream", i)))
		}
		starts = append(starts, raw[0])
	}
	if !hasDelta(starts, 8) {
		t.Error(test.DiffMessage(starts, "<consecutive messages 8 stream bytes apart>", "R3: a message under a connection consumes 8 bytes"))
	}
}

func TestNewMessage_WithoutConnectionReads18Bytes(t *testing.T) {
	installReader(t, &streamReader{})
	resetSources()

	starts := make([]byte, 0, 100)
	for i := range 100 {
		_, s := NewMessage(t.Context(), fixedNow)
		raw := mustDecode(t, s.TraceID[12:]+s.Message)
		if !isConsecutive(raw) {
			t.Fatal(test.DiffMessage(hex.EncodeToString(raw), "<18 consecutive stream values>", fmt.Sprintf("R3: message %d: the ID's bytes follow the trace tail's", i)))
		}
		starts = append(starts, raw[0])
	}
	if !hasDelta(starts, 18) {
		t.Error(test.DiffMessage(starts, "<consecutive messages 18 stream bytes apart>", "R3: a message without a connection consumes 18 bytes"))
	}
}

func TestScope_TraceparentIsHeaderValue(t *testing.T) {
	for _, h := range []http.Header{nil, headerOf("Traceparent", validHeader)} {
		_, s := NewRequest(t.Context(), h, fixedNow)
		_, traceparent := s.Headers()
		if len(traceparent) != 1 || s.Traceparent() != traceparent[0] {
			t.Error(test.DiffMessage(s.Traceparent(), traceparent, "R6: Traceparent() equals the second slice of Headers()"))
		}
	}
	_, m := NewMessage(t.Context(), fixedNow)
	_, traceparent := m.Headers()
	if m.Traceparent() == "" || len(traceparent) != 1 || traceparent[0] != m.Traceparent() {
		t.Error(test.DiffMessage(m.Traceparent(), traceparent, "R6: a message's Headers() carries its traceparent"))
	}
}

func TestNewRequest_TwoAllocations(t *testing.T) {
	for _, h := range []http.Header{nil, headerOf("Traceparent", validHeader, "X-Request-Id", "abc")} {
		if got := testing.AllocsPerRun(500, func() { NewRequest(context.Background(), h, fixedNow) }); got > 2 {
			t.Error(test.DiffMessage(got, "<= 2", "R8: NewRequest allocates the scope context and the traceparent string only"))
		}
	}
}

func TestNewRequest_RedrawsZeroRequestID(t *testing.T) {
	block := patternBlock(repeated(0x11, 10), repeated(0, 8))
	for range 28 {
		block = append(block, patternBlock(repeated(0x22, 18))...)
	}
	block = block[:504]
	installReader(t, &blockReader{blocks: [][]byte{block}})
	resetSources()

	_, s := NewRequest(t.Context(), nil, fixedNow)
	if s.Request == "0000000000000000" {
		t.Fatal(test.DiffMessage(s.Request, "<not all zeros>", "R4: a zero request ID is drawn again"))
	}
	if want := "2222222222222222"; s.Request != want {
		t.Error(test.DiffMessage(s.Request, want, "R4: the whole 18-byte read is drawn again"))
	}
	if want := "22222222222222222222"; s.TraceID[12:] != want {
		t.Error(test.DiffMessage(s.TraceID[12:], want, "R4: the trace tail comes from the new draw too"))
	}
}

func TestNewRequest_RedrawsZeroTraceTail(t *testing.T) {
	block := patternBlock(repeated(0, 10), repeated(0x11, 8))
	for range 28 {
		block = append(block, repeated(0x22, 18)...)
	}
	block = block[:504]
	installReader(t, &blockReader{blocks: [][]byte{block}})
	resetSources()

	_, s := NewRequest(t.Context(), nil, fixedNow)
	if want := "22222222222222222222"; s.TraceID[12:] != want {
		t.Error(test.DiffMessage(s.TraceID[12:], want, "R4: an all-zero trace tail is drawn again"))
	}
	if want := "2222222222222222"; s.Request != want {
		t.Error(test.DiffMessage(s.Request, want, "R4: the request ID comes from the new draw"))
	}
}

func TestNewRequest_RedrawsAllZeroBlock(t *testing.T) {
	installReader(t, &zeroFirstReader{})
	resetSources()

	_, s := NewRequest(t.Context(), nil, fixedNow)
	if s.Request == "0000000000000000" || s.TraceID[12:] == "00000000000000000000" {
		t.Error(test.DiffMessage([]string{s.Request, s.TraceID[12:]}, "<non-zero>", "R4: zeros from the reader never reach an ID"))
	}
}
