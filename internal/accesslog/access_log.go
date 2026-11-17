package accesslog

import (
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/trace"
)

const (
	maxStagesPerRequest = 64
	maxPending          = 16384
	shardCount          = 64
	shardCapacity       = maxPending / shardCount
	inlineStages        = 8
	inlineText          = 512
	inlineAttrs         = 16
	maxTargetBytes      = 2048
)

type AccessLogConfig struct {
	Tracer *trace.Tracer
	Logger common.LogAdapter
}

type AccessLog struct {
	logger     common.LogAdapter
	builtin    accessLogger
	stopListen func()
	shards     [shardCount]shard
	stopped    atomic.Bool
	stopOnce   sync.Once
}

type accessLogger interface {
	LogAccess(line logpipe.AccessLine)
}

type stageEntry struct {
	stage    string
	name     string
	duration time.Duration
}

type record struct {
	id      string
	stages  []stageEntry
	dropped int
	prev    *record
	next    *record
	inline  [inlineStages]stageEntry
}

type shard struct {
	mu     sync.Mutex
	byID   map[string]*record
	oldest *record
	newest *record
}

type stageText struct {
	key        string
	start, end int
}

func NewAccessLog(cfg *AccessLogConfig) *AccessLog {
	if cfg == nil || cfg.Tracer == nil || cfg.Logger == nil {
		panic(color.FmtRed("invalid access log: a tracer and a logger are required"))
	}
	al := &AccessLog{logger: cfg.Logger}
	al.builtin, _ = cfg.Logger.(accessLogger)
	for i := range al.shards {
		al.shards[i].byID = make(map[string]*record)
	}
	al.stopListen = cfg.Tracer.Listen(al.listen)
	return al
}

func (al *AccessLog) Stop() {
	al.stopOnce.Do(func() {
		al.stopped.Store(true)
		al.stopListen()
		for i := range al.shards {
			s := &al.shards[i]
			s.mu.Lock()
			clear(s.byID)
			s.oldest, s.newest = nil, nil
			s.mu.Unlock()
		}
	})
}

func (al *AccessLog) pendingLen() int {
	n := 0
	for i := range al.shards {
		s := &al.shards[i]
		s.mu.Lock()
		n += len(s.byID)
		s.mu.Unlock()
	}
	return n
}

func (al *AccessLog) listen(e trace.Event) {
	if al.stopped.Load() {
		return
	}
	s := &al.shards[shardIndex(e.ID)]
	if e.Stage == trace.StageComplete {
		s.mu.Lock()
		rec := s.byID[e.ID]
		if rec != nil {
			s.removeLocked(rec)
		}
		s.mu.Unlock()
		al.write(e, rec)
		return
	}
	s.mu.Lock()
	rec := s.byID[e.ID]
	if rec == nil {
		if len(s.byID) >= shardCapacity {
			s.removeLocked(s.oldest)
		}
		rec = &record{id: e.ID}
		rec.stages = rec.inline[:0]
		s.addLocked(rec)
	}
	if len(rec.stages) < maxStagesPerRequest {
		rec.stages = append(rec.stages, stageEntry{stage: e.Stage, name: e.Name, duration: e.Duration})
	} else {
		rec.dropped++
	}
	s.mu.Unlock()
}

func shardIndex(id string) int {
	n := len(id)
	if n == 0 {
		return 0
	}
	h := uint32(id[n-1])
	if n > 1 {
		h |= uint32(id[n-2]) << 8
	}
	return int((h * 0x9e3779b1) >> 26)
}

func (s *shard) addLocked(rec *record) {
	s.byID[rec.id] = rec
	rec.prev = s.newest
	if s.newest != nil {
		s.newest.next = rec
	} else {
		s.oldest = rec
	}
	s.newest = rec
}

func (s *shard) removeLocked(rec *record) {
	delete(s.byID, rec.id)
	if rec.prev != nil {
		rec.prev.next = rec.next
	} else {
		s.oldest = rec.next
	}
	if rec.next != nil {
		rec.next.prev = rec.prev
	} else {
		s.newest = rec.prev
	}
	rec.prev, rec.next = nil, nil
}

func (al *AccessLog) write(e trace.Event, rec *record) {
	defer func() { _ = recover() }()
	var stages []stageEntry
	var stagesDropped int
	if rec != nil {
		stages = rec.stages
		stagesDropped = rec.dropped
	}
	var textBuf [inlineText]byte
	var partBuf [inlineStages]stageText
	buf, parts := appendStageTexts(textBuf[:0], partBuf[:0], stages)
	ipStart := len(buf)
	if e.ClientIP.IsValid() {
		buf = e.ClientIP.AppendTo(buf)
	}
	totalStart := len(buf)
	buf = appendDuration(buf, e.Duration)
	text := string(buf)
	ip := text[ipStart:totalStart]
	total := text[totalStart:]
	msg := cutTarget(e.Target)

	if al.builtin != nil {
		var attrBuf [inlineAttrs]slog.Attr
		attrs := appendIDAttr(attrBuf[:0], "trace_id", e.TraceID)
		if e.Transport == trace.TransportWS {
			attrs = appendIDAttr(attrs, "connection", e.ParentID)
			attrs = appendIDAttr(attrs, "message", e.ID)
		} else {
			attrs = appendIDAttr(attrs, "request", e.ID)
		}
		attrs = appendIDAttr(attrs, "ip", ip)
		attrs = append(attrs, slog.String("transport", e.Transport), slog.Int("code", e.Code), slog.String("operation", e.Operation))
		for _, p := range parts {
			attrs = append(attrs, slog.String(p.key, text[p.start:p.end]))
		}
		if stagesDropped > 0 {
			attrs = append(attrs, slog.Int("stages_dropped", stagesDropped))
		}
		attrs = append(attrs, slog.String("total", total))
		al.builtin.LogAccess(logpipe.AccessLine{Msg: msg, Attrs: attrs})
		return
	}

	size := 16 + 2*len(parts)
	if stagesDropped > 0 {
		size += 2
	}
	args := appendID(make([]any, 0, size), "trace_id", e.TraceID)
	if e.Transport == trace.TransportWS {
		args = appendID(args, "connection", e.ParentID)
		args = appendID(args, "message", e.ID)
	} else {
		args = appendID(args, "request", e.ID)
	}
	args = appendID(args, "ip", ip)
	args = append(args, "transport", e.Transport, "code", e.Code, "operation", e.Operation)
	for _, p := range parts {
		args = append(args, p.key, text[p.start:p.end])
	}
	if stagesDropped > 0 {
		args = append(args, "stages_dropped", stagesDropped)
	}
	args = append(args, "total", total)
	al.logger.Info(msg, args...)
}

func appendIDAttr(attrs []slog.Attr, key, value string) []slog.Attr {
	if value == "" {
		return attrs
	}
	return append(attrs, slog.String(key, value))
}

func appendID(args []any, key, value string) []any {
	if value == "" {
		return args
	}
	return append(args, key, value)
}

func appendStageTexts(buf []byte, parts []stageText, stages []stageEntry) ([]byte, []stageText) {
	if len(stages) == 0 {
		return buf, parts
	}
	var percentBuf [maxStagesPerRequest]int
	percents := percentBuf[:len(stages)]
	distributePercentages(stages, percents)
	var written [maxStagesPerRequest]bool
	for i, first := range stages {
		if written[i] {
			continue
		}
		start := len(buf)
		for j := i; j < len(stages); j++ {
			s := stages[j]
			if written[j] || s.stage != first.stage {
				continue
			}
			written[j] = true
			if len(buf) > start {
				buf = append(buf, ", "...)
			}
			buf = append(buf, shortName(s.name)...)
			buf = append(buf, ' ')
			buf = appendDuration(buf, s.duration)
			buf = append(buf, " ("...)
			buf = strconv.AppendInt(buf, int64(percents[j]), 10)
			buf = append(buf, "%)"...)
		}
		parts = append(parts, stageText{key: first.stage, start: start, end: len(buf)})
	}
	return buf, parts
}

func cutTarget(target string) string {
	if len(target) <= maxTargetBytes {
		return target
	}
	i := maxTargetBytes
	for i > 0 && !utf8.RuneStart(target[i]) {
		i--
	}
	return target[:i] + "..."
}

func distributePercentages(stages []stageEntry, percents []int) {
	var total time.Duration
	for _, s := range stages {
		total += s.duration
	}
	if total <= 0 {
		return
	}

	var remainderBuf [maxStagesPerRequest]float64
	remainders := remainderBuf[:len(stages)]
	sumFloor := 0
	for i, s := range stages {
		exact := float64(s.duration) / float64(total) * 100
		floor := int(exact)
		percents[i] = floor
		remainders[i] = exact - float64(floor)
		sumFloor += floor
	}

	diff := 100 - sumFloor
	step, count := 1, diff
	if diff < 0 {
		step, count = -1, -diff
	}
	var used [maxStagesPerRequest]bool
	for range min(count, len(stages)) {
		pick := -1
		for i, r := range remainders {
			if used[i] {
				continue
			}
			if pick < 0 || (step > 0 && r > remainders[pick]) || (step < 0 && r < remainders[pick]) {
				pick = i
			}
		}
		used[pick] = true
		percents[pick] += step
	}
}

func formatDuration(d time.Duration) string {
	return string(appendDuration(nil, d))
}

func appendDuration(buf []byte, d time.Duration) []byte {
	switch {
	case d < time.Microsecond:
	case d < time.Millisecond:
		d = d.Round(time.Microsecond / 100)
	case d < time.Second:
		d = d.Round(time.Millisecond / 100)
	default:
		d = d.Round(time.Second / 100)
	}
	var text [32]byte
	return append(buf, text[durationText(&text, d):]...)
}

func durationText(buf *[32]byte, d time.Duration) int {
	w := len(buf)
	u := uint64(d)
	neg := d < 0
	if neg {
		u = -u
	}
	if u < uint64(time.Second) {
		var prec int
		w--
		buf[w] = 's'
		w--
		switch {
		case u == 0:
			buf[w] = '0'
			return w
		case u < uint64(time.Microsecond):
			buf[w] = 'n'
		case u < uint64(time.Millisecond):
			prec = 3
			w--
			copy(buf[w:], "µ")
		default:
			prec = 6
			buf[w] = 'm'
		}
		w, u = durationFrac(buf[:w], u, prec)
		w = durationInt(buf[:w], u)
	} else {
		w--
		buf[w] = 's'
		w, u = durationFrac(buf[:w], u, 9)
		w = durationInt(buf[:w], u%60)
		u /= 60
		if u > 0 {
			w--
			buf[w] = 'm'
			w = durationInt(buf[:w], u%60)
			u /= 60
			if u > 0 {
				w--
				buf[w] = 'h'
				w = durationInt(buf[:w], u)
			}
		}
	}
	if neg {
		w--
		buf[w] = '-'
	}
	return w
}

func durationFrac(buf []byte, v uint64, prec int) (int, uint64) {
	w := len(buf)
	printed := false
	for range prec {
		digit := v % 10
		printed = printed || digit != 0
		if printed {
			w--
			buf[w] = byte(digit) + '0'
		}
		v /= 10
	}
	if printed {
		w--
		buf[w] = '.'
	}
	return w, v
}

func durationInt(buf []byte, v uint64) int {
	w := len(buf)
	if v == 0 {
		w--
		buf[w] = '0'
		return w
	}
	for v > 0 {
		w--
		buf[w] = byte(v%10) + '0'
		v /= 10
	}
	return w
}

func colorForDuration(d time.Duration) color.Color {
	return color.ForDuration(d)
}

func shortName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
