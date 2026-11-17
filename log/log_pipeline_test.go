package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/test"
)

type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuf) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

type holdWriter struct {
	mu      sync.Mutex
	gate    chan struct{}
	entered chan struct{}
	buf     bytes.Buffer
}

func newHoldWriter() *holdWriter {
	return &holdWriter{entered: make(chan struct{}, 1<<12), gate: make(chan struct{})}
}

func (w *holdWriter) openAll() {
	w.mu.Lock()
	if w.gate != nil {
		close(w.gate)
		w.gate = nil
	}
	w.mu.Unlock()
}

func (w *holdWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	gate := w.gate
	w.mu.Unlock()
	select {
	case w.entered <- struct{}{}:
	default:
	}
	if gate != nil {
		<-gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *holdWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func asyncLogger(t *testing.T, w io.Writer, opts LogOptions) (Logger, *logpipe.Sink) {
	t.Helper()
	s := logpipe.New(w)
	s.StartAsync()
	t.Cleanup(func() {
		if h, ok := w.(*holdWriter); ok {
			h.openAll()
		}
		s.StopAsync(time.Minute)
	})
	return newLogSink(s, false, &opts), s
}

func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal(test.DiffMessage("blocked for 30s", "returned", what))
	}
}

func goroutinesAtMost(base int) bool {
	for range 200000 {
		if runtime.NumGoroutine() <= base {
			return true
		}
		runtime.Gosched()
	}
	return runtime.NumGoroutine() <= base
}

func TestPipeline_ValueChangedAfterCallNotSeen(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	l, s := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	m := map[string]any{"k": "before"}
	list := []string{"before"}
	text := "before"
	l.Info("m", "map", m, "list", list, "ptr", &text)
	m["k"] = "after"
	list[0] = "after"
	text = "after"
	w.openAll()
	if !s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
	}
	out := w.String()
	if strings.Contains(out, "after") || strings.Count(out, "before") != 3 {
		t.Error(test.DiffMessage(out, "three before, no after", "P1: values are read during the call only"))
	}
}

type pipeCountingValuer struct{ calls *int }

func (c pipeCountingValuer) LogValue() slog.Value {
	*c.calls++
	return slog.StringValue("resolved")
}

func TestPipeline_LogValueResolvedDuringCall(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	l, _ := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	calls := 0
	l.Info("m", "v", pipeCountingValuer{&calls})
	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "P1: a LogValuer is resolved on the caller, before Info returns"))
	}
}

func TestPipeline_TwoLoggersShareOrder(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	s := logpipe.New(w)
	s.StartAsync()
	t.Cleanup(func() {
		w.openAll()
		s.StopAsync(time.Minute)
	})
	a := newLogSink(s, false, &LogOptions{Format: FormatJSON})
	b := newLogSink(s, false, &LogOptions{Format: FormatText})
	for i := range 200 {
		if i%2 == 0 {
			a.Info("n", "n", i)
		} else {
			b.Info("n", "n", i)
		}
	}
	w.openAll()
	if !s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
	}
	lines := strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n")
	if len(lines) != 200 {
		t.Fatal(test.DiffMessage(len(lines), 200, "P2: loggers on one sink share its queue"))
	}
	for i, line := range lines {
		if !strings.HasSuffix(line, `"n":`+strconv.Itoa(i)+"}") && !strings.HasSuffix(line, "n="+strconv.Itoa(i)) {
			t.Fatal(test.DiffMessage(line, "record "+strconv.Itoa(i), "O1: records of one goroutine are written in call order across loggers"))
		}
	}
}

func TestPipeline_ConcurrentWritersWholeRecords(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	l, s := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	const goroutines, per = 16, 250
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				l.Info("n", "g", g, "i", i, "pad", strings.Repeat("p", (g+i)%20))
			}
		}()
	}
	wg.Wait()
	w.openAll()
	if !s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
	}
	seen := map[[2]int]bool{}
	last := make([]int, goroutines)
	for i := range last {
		last[i] = -1
	}
	for _, line := range strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n") {
		var rec struct{ G, I int }
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatal(test.DiffMessage(line, "one JSON object", "P6: records are written whole"))
		}
		rec.G, rec.I = int(raw["g"].(float64)), int(raw["i"].(float64))
		if seen[[2]int{rec.G, rec.I}] || rec.I <= last[rec.G] {
			t.Fatal(test.DiffMessage(rec, "once and in order per goroutine", "O1: no duplicate, per-goroutine order kept"))
		}
		seen[[2]int{rec.G, rec.I}] = true
		last[rec.G] = rec.I
	}
	if len(seen) != goroutines*per {
		t.Error(test.DiffMessage(len(seen), goroutines*per, "P5: every record written"))
	}
}

func textOverhead(t *testing.T) int {
	t.Helper()
	buf := &bytes.Buffer{}
	l := newLogSink(logpipe.New(buf), false, &LogOptions{Format: FormatText})
	l.Info("a")
	return buf.Len() - 1
}

func textLineOfLength(t *testing.T, total int) string {
	t.Helper()
	buf := &bytes.Buffer{}
	l := newLogSink(logpipe.New(buf), false, &LogOptions{Format: FormatText})
	l.Info(strings.Repeat("a", total-textOverhead(t)))
	return buf.String()
}

func TestPipeline_RecordAtLimitNotTruncated(t *testing.T) {
	const maxRecord = 64 << 10
	for _, total := range []int{maxRecord - 1, maxRecord} {
		out := textLineOfLength(t, total)
		if len(out) != total || strings.Contains(out, "[TRUNCATED]") {
			t.Error(test.DiffMessage(len(out), total, "P3: a record of "+strconv.Itoa(total)+" bytes is not cut"))
		}
	}
}

func TestPipeline_LongRecordTruncated(t *testing.T) {
	const maxRecord = 64 << 10
	out := textLineOfLength(t, maxRecord+1)
	if len(out) > maxRecord || !strings.HasSuffix(out, "...[TRUNCATED]\n") || len(out) < maxRecord-64 {
		t.Error(test.DiffMessage(len(out), "<= 65536 and ends with ...[TRUNCATED]", "P3: maxRecord + 1 is cut to at most maxRecord bytes"))
	}
	if !strings.HasPrefix(out, "time=") || !strings.Contains(out, "level=INFO msg=aaaa") {
		t.Error(test.DiffMessage(out[:60], "the head survives", "P3: the head of the record is kept"))
	}

	buf := &bytes.Buffer{}
	jsonLogger := newLogSink(logpipe.New(buf), false, &LogOptions{Format: FormatJSON})
	jsonLogger.Info("m", "k", strings.Repeat("y", 200000))
	jsonLogger.Info("next")
	lines := strings.SplitAfter(buf.String(), "\n")
	if len(lines) != 3 || len(lines[0]) > maxRecord || !strings.HasSuffix(lines[0], "...[TRUNCATED]\n") || json.Valid([]byte(lines[0])) {
		t.Fatal(test.DiffMessage(len(lines[0]), "<= 65536, ends with ...[TRUNCATED], not valid JSON", "P3: a cut JSON record"))
	}
	if !strings.HasPrefix(lines[0], `{"time":"`) || !strings.Contains(lines[0], `"level":"INFO","msg":"m"`) {
		t.Error(test.DiffMessage(lines[0][:80], "time, level and message", "P3: the head with time, level and message survives"))
	}
	if !strings.Contains(lines[1], `"msg":"next"`) || !json.Valid([]byte(lines[1])) {
		t.Error(test.DiffMessage(lines[1], "the next record, whole", "P3: the record after a cut one is whole"))
	}

	prettyBuf := &bytes.Buffer{}
	pretty := newLogSink(logpipe.New(prettyBuf), false, &LogOptions{Format: FormatPretty})
	pretty.Info("m", "k", strings.Repeat("y", 200000))
	if got := prettyBuf.String(); len(got) > maxRecord || !strings.HasSuffix(got, ansiReset+"...[TRUNCATED]\n") {
		t.Error(test.DiffMessage(len(got), "<= 65536, a color reset then ...[TRUNCATED]", "P3: Pretty resets the color before the marker"))
	}
}

func TestPipeline_TruncateOnRuneBoundary(t *testing.T) {
	const maxRecord = 64 << 10
	for pad := range 8 {
		for _, format := range []Format{FormatJSON, FormatText, FormatPretty} {
			buf := &bytes.Buffer{}
			l := newLogSink(logpipe.New(buf), false, &LogOptions{Format: format})
			l.Info(strings.Repeat("a", pad) + strings.Repeat("😀", 30000))
			out := buf.String()
			if !utf8.ValidString(out) || len(out) > maxRecord || !strings.HasSuffix(out, "...[TRUNCATED]\n") {
				t.Fatal(test.DiffMessage(len(out), "valid UTF-8, <= 65536, truncated", fmt.Sprintf("P3: the cut falls on a UTF-8 boundary, %s pad %d", formatName(format), pad)))
			}
		}
	}
}

func TestPipeline_DropReportRecord(t *testing.T) {
	big := strings.Repeat("x", 60000)
	for _, format := range []Format{FormatJSON, FormatText} {
		w := newHoldWriter()
		t.Cleanup(w.openAll)
		l, s := asyncLogger(t, w, LogOptions{Format: format, Level: LevelDebug})
		const total = 26
		for i := range total {
			if i%2 == 0 {
				l.Debug("big", "pad", big)
			} else {
				l.Info("big", "pad", big)
			}
		}
		w.openAll()
		if !s.Flush(time.Minute) {
			t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
		}
		out := w.String()
		var report *regexp.Regexp
		if format == FormatJSON {
			report = regexp.MustCompile(`"level":"WARN","msg":"LogDropped","debug":(\d+),"info":(\d+)\}`)
		} else {
			report = regexp.MustCompile(`level=WARN msg=LogDropped debug=(\d+) info=(\d+)`)
		}
		m := report.FindAllStringSubmatch(out, -1)
		if len(m) != 1 {
			t.Fatal(test.DiffMessage(len(m), 1, "P10: one WARN LogDropped record with a pair per level, debug then info, "+formatName(format)))
		}
		debug, _ := strconv.Atoi(m[0][1])
		info, _ := strconv.Atoi(m[0][2])
		accepted := strings.Count(out, "big")
		if accepted+debug+info != total || debug == 0 || info == 0 {
			t.Error(test.DiffMessage(accepted+debug+info, total, "P10: written plus reported equal submitted, "+formatName(format)))
		}
	}
}

func TestNewLog_StdoutSinkSynchronousWithoutApp(t *testing.T) {
	buf := &syncBuf{}
	restore := logpipe.SetStdout(buf)
	defer restore()
	runtime.GC()
	base := runtime.NumGoroutine()
	a := NewLog(&LogOptions{Format: FormatJSON})
	b := NewLog(&LogOptions{Format: FormatText})
	for i := range 100 {
		a.Info("a", "i", i)
		if !strings.Contains(buf.String(), `"i":`+strconv.Itoa(i)+"}") {
			t.Fatal(test.DiffMessage(buf.String(), "the line just logged", "M5, P4: a logger used without an app writes before the call returns"))
		}
		b.Info("b", "i", i)
	}
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "M5: no goroutine is started without an app"))
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 200 || !strings.HasPrefix(lines[0], "{") || !strings.HasPrefix(lines[1], "time=") {
		t.Error(test.DiffMessage(len(lines), 200, "P2: every logger built by NewLog writes to the one stdout sink, in order"))
	}
}

func TestNewLog_AsyncOnceStdoutSinkStarted(t *testing.T) {
	w := newHoldWriter()
	restore := logpipe.SetStdout(w)
	defer restore()
	logpipe.Stdout().StartAsync()
	defer logpipe.Stdout().StopAsync(time.Minute)
	defer w.openAll()
	l := NewLog(&LogOptions{Format: FormatJSON})
	within(t, "P5: a built-in logger does not wait for stdout in asynchronous mode", func() {
		for i := range 20 {
			l.Info("held", "i", i)
		}
	})
	if w.String() != "" {
		t.Error(test.DiffMessage(w.String(), "", "P5: nothing reached the held stdout"))
	}
	w.openAll()
	if !logpipe.Stdout().Flush(time.Minute) || strings.Count(w.String(), `"msg":"held"`) != 20 {
		t.Error(test.DiffMessage(strings.Count(w.String(), `"msg":"held"`), 20, "P2: the lines reach stdout through the process-wide sink"))
	}
}

func TestWrapLogger_AdapterCalledDirectlyInAsyncMode(t *testing.T) {
	w := newHoldWriter()
	restore := logpipe.SetStdout(w)
	defer restore()
	logpipe.Stdout().StartAsync()
	defer logpipe.Stdout().StopAsync(time.Minute)
	defer w.openAll()
	adapter := &fromAdapter{}
	l := WrapLogger(adapter, nil)
	l.Info("direct", "k", 1)
	if calls := adapter.all(); len(calls) != 1 {
		t.Error(test.DiffMessage(len(calls), 1, "O4: an adapter is called on the caller, before the call returns, in every mode"))
	}
}

type exitProbe struct {
	mu    sync.Mutex
	codes []int
	seen  []string
	on    func() string
	ch    chan int
}

func newExitProbe(on func() string) *exitProbe {
	return &exitProbe{on: on, ch: make(chan int, 4)}
}

func (e *exitProbe) install(t *testing.T) {
	t.Helper()
	saved := exit
	exit = func(code int) {
		e.mu.Lock()
		e.codes = append(e.codes, code)
		if e.on != nil {
			e.seen = append(e.seen, e.on())
		}
		e.mu.Unlock()
		e.ch <- code
	}
	t.Cleanup(func() { exit = saved })
}

func TestFatal_FlushesThenExits(t *testing.T) {
	w := &syncBuf{}
	l, _ := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	probe := newExitProbe(w.String)
	probe.install(t)
	l.Info("one")
	l.Info("two")
	l.Fatal("bye", "k", 1)
	if len(probe.codes) != 1 || probe.codes[0] != 1 {
		t.Fatal(test.DiffMessage(probe.codes, []int{1}, "X1: Fatal exits with 1 once"))
	}
	at := probe.seen[0]
	i, j, k := strings.Index(at, `"msg":"one"`), strings.Index(at, `"msg":"two"`), strings.Index(at, `"level":"FATAL","msg":"bye"`)
	if i < 0 || j < i || k < j {
		t.Error(test.DiffMessage(at, "one, two, FATAL bye, all written before exit", "X1: the sink is flushed before exit"))
	}
}

func fatalOnStuckOutput(t *testing.T, async bool) {
	t.Helper()
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	s := logpipe.New(w)
	if async {
		s.StartAsync()
	}
	t.Cleanup(func() {
		w.openAll()
		if async {
			s.StopAsync(time.Minute)
		}
	})
	l := newLogSink(s, false, &LogOptions{Format: FormatJSON})
	probe := newExitProbe(nil)
	probe.install(t)
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		l.Fatal("bye")
	}()
	select {
	case code := <-probe.ch:
		if code != 1 {
			t.Error(test.DiffMessage(code, 1, "X1: exit code"))
		}
	case <-time.After(30 * time.Second):
		t.Fatal(test.DiffMessage("Fatal still blocked after 30s", "exit(1) after the flush timeout", "bug 1, X1: Fatal never hangs on a stuck output"))
	}
	<-returned
}

func TestFatal_StuckOutputExitsAfterTimeout_Sync(t *testing.T) {
	fatalOnStuckOutput(t, false)
}

func TestFatal_StuckOutputExitsAfterTimeout_Async(t *testing.T) {
	fatalOnStuckOutput(t, true)
}

type fatalAdapter struct {
	*fromAdapter
	onFatal func()
	fatals  int
}

func (f *fatalAdapter) Fatal(msg string, args ...any) {
	f.fatals++
	if f.onFatal != nil {
		f.onFatal()
	}
	f.fromAdapter.Fatal(msg, args...)
}

func TestWrapLogger_FatalFlushesStdoutSink(t *testing.T) {
	buf := &syncBuf{}
	restore := logpipe.SetStdout(buf)
	defer restore()
	logpipe.Stdout().StartAsync()
	defer logpipe.Stdout().StopAsync(time.Minute)
	builtin := NewLog(&LogOptions{Format: FormatJSON})
	builtin.Info("before-fatal")
	var seen string
	adapter := &fatalAdapter{fromAdapter: &fromAdapter{}, onFatal: func() { seen = buf.String() }}
	WrapLogger(adapter, nil).Fatal("bye", "k", 1)
	if adapter.fatals != 1 || len(adapter.all()) != 1 || adapter.all()[0].msg != "bye" {
		t.Error(test.DiffMessage(adapter.fatals, 1, "X2: the adapter's Fatal is called once"))
	}
	if !strings.Contains(seen, "before-fatal") {
		t.Error(test.DiffMessage(seen, "the line logged before", "X2: the stdout sink is flushed before the adapter's Fatal"))
	}
}

func TestPretty_UserRecordDurationsNeverColored(t *testing.T) {
	out := prettyOnce("GET /x 5ms (4%)", "middleware", "CORS 5ms (4%), CSRF 2s (96%)", "total", "3s")
	if strings.Contains(out, string(color.Gray)) || strings.Contains(out, string(color.Red)+"3s") || strings.Contains(out, string(color.Yellow)) {
		t.Error(test.DiffMessage(out, "no duration color", "A2: a user value that looks like an access line is not colored"))
	}
	if !strings.Contains(out, ansiGreen+"CORS 5ms (4%), CSRF 2s (96%)"+ansiReset) {
		t.Error(test.DiffMessage(out, "the whole value as one green string", "A2: user records are never colored by their durations"))
	}
}
