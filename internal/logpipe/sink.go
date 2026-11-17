package logpipe

import (
	"bytes"
	"io"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dangduoc08/ginject/internal/inspect"
)

type Level uint8

const (
	Debug Level = iota
	Info
	Warn
	Error
	Fatal
)

const (
	maxRecord      = 64 << 10
	queueLimit     = 1 << 20
	softLimit      = queueLimit * 8 / 10
	batchSize      = 64 << 10
	fullWait       = 100 * time.Millisecond
	reportInterval = time.Second
	keepCapacity   = 64 << 10
	minCapacity    = 4 << 10

	FlushTimeout = 2 * time.Second
)

const (
	wakeDelay = time.Millisecond
	wakeSize  = 16 << 10
)

type wakeTimer interface {
	Reset(d time.Duration) bool
	Stop() bool
}

const (
	truncatedMarker = "...[TRUNCATED]\n"
	colorReset      = "\x1b[0m"
)

type Sink struct {
	mu        sync.Mutex
	out       io.Writer
	queue     []byte
	ends      []int
	spare     []byte
	spareEnds []int

	accepted uint64
	done     uint64
	async    int
	writing  bool
	stuck    bool
	progress chan struct{}

	dropped       [4]uint64
	dropSeq       uint64
	takenSeq      uint64
	reportedSeq   uint64
	pendingSeq    uint64
	pendingPos    uint64
	reportPending bool
	report        func(dropped [4]uint64) []byte
	lastReport    time.Time

	timer    wakeTimer
	armed    bool
	flushing int
	wakes    uint64
	writes   uint64

	now      func() time.Time
	after    func(time.Duration) <-chan time.Time
	newTimer func(d time.Duration, f func()) wakeTimer
}

var stdout = New(os.Stdout)

func Stdout() *Sink {
	return stdout
}

func New(w io.Writer) *Sink {
	return &Sink{out: w, now: time.Now, after: time.After, newTimer: newWakeTimer}
}

func newWakeTimer(d time.Duration, f func()) wakeTimer {
	return time.AfterFunc(d, f)
}

func SetStdout(w io.Writer) (restore func()) {
	s := stdout
	s.Flush(FlushTimeout)
	s.mu.Lock()
	out, async := s.out, s.async
	s.out, s.async = w, 0
	s.syncLocked()
	s.mu.Unlock()
	return func() {
		s.Flush(FlushTimeout)
		s.mu.Lock()
		s.out, s.async = out, async
		if async == 0 {
			s.syncLocked()
		}
		s.mu.Unlock()
	}
}

func (s *Sink) StartAsync() {
	s.mu.Lock()
	if s.async == 0 {
		s.lastReport = s.now()
	}
	s.async++
	s.mu.Unlock()
}

func (s *Sink) StopAsync(timeout time.Duration) bool {
	ok := s.Flush(timeout)
	s.mu.Lock()
	if s.async > 0 {
		s.async--
		if s.async == 0 {
			s.syncLocked()
		}
	}
	s.mu.Unlock()
	return ok
}

func (s *Sink) syncLocked() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.armed = false
	if len(s.queue) > 0 && !s.writing {
		s.writing = true
		s.wakes++
		go s.drain()
	}
}

func (s *Sink) Write(level Level, record []byte, report func(dropped [4]uint64) []byte) {
	s.mu.Lock()
	if s.async == 0 && level != Fatal {
		s.appendLocked(record)
		if s.writing {
			s.waitLocked(s.accepted)
			s.mu.Unlock()
			return
		}
		s.writing = true
		s.drainLocked()
		return
	}
	n := uint64(min(len(record), maxRecord))
	switch {
	case level == Fatal:
	case level <= Info:
		if s.accepted-s.done+n > softLimit {
			s.dropLocked(level, report)
			s.mu.Unlock()
			return
		}
	case s.accepted-s.done+n > queueLimit:
		if s.stuck || !s.waitRoomLocked(n) {
			s.dropLocked(level, report)
			s.mu.Unlock()
			return
		}
	}
	s.appendLocked(record)
	switch {
	case s.writing:
	case level == Fatal || len(s.queue) >= wakeSize:
		s.writing = true
		s.wakes++
		go s.drain()
	default:
		s.armLocked()
	}
	s.mu.Unlock()
}

func (s *Sink) armLocked() {
	if s.armed {
		return
	}
	s.armed = true
	if s.timer == nil {
		s.timer = s.newTimer(wakeDelay, s.fire)
		return
	}
	s.timer.Reset(wakeDelay)
}

func (s *Sink) fire() {
	s.mu.Lock()
	s.armed = false
	if s.writing || len(s.queue) == 0 {
		s.mu.Unlock()
		return
	}
	s.writing = true
	s.wakes++
	s.drainLocked()
}

func (s *Sink) Flush(timeout time.Duration) bool {
	s.mu.Lock()
	s.flushing++
	target, dropTarget := s.accepted, s.dropSeq
	if !s.writing && (len(s.queue) > 0 || s.dropSeq != s.takenSeq) {
		s.writing = true
		s.wakes++
		go s.drain()
	}
	var expired <-chan time.Time
	extended := false
	for {
		if s.done >= target && s.reportedSeq >= dropTarget {
			taken := s.accepted - uint64(len(s.queue))
			if extended || taken <= target {
				break
			}
			extended = true
			target = taken
			continue
		}
		ch := s.progressLocked()
		if expired == nil {
			expired = s.after(timeout)
		}
		s.mu.Unlock()
		select {
		case <-ch:
		case <-expired:
			s.mu.Lock()
			s.flushing--
			s.mu.Unlock()
			return false
		}
		s.mu.Lock()
	}
	s.flushing--
	s.mu.Unlock()
	return true
}

func (s *Sink) dropLocked(level Level, report func([4]uint64) []byte) {
	s.dropped[level]++
	s.dropSeq++
	s.report = report
}

func (s *Sink) waitLocked(pos uint64) {
	for s.done < pos {
		ch := s.progressLocked()
		s.mu.Unlock()
		<-ch
		s.mu.Lock()
	}
}

func (s *Sink) waitRoomLocked(n uint64) bool {
	expired := s.after(fullWait)
	for {
		ch := s.progressLocked()
		s.mu.Unlock()
		select {
		case <-ch:
		case <-expired:
			s.mu.Lock()
			s.stuck = true
			return false
		}
		s.mu.Lock()
		if s.accepted-s.done+n <= queueLimit {
			return true
		}
	}
}

func (s *Sink) progressLocked() chan struct{} {
	if s.progress == nil {
		s.progress = make(chan struct{})
	}
	return s.progress
}

func (s *Sink) signalLocked() {
	if s.progress != nil {
		close(s.progress)
		s.progress = nil
	}
}

func (s *Sink) appendLocked(record []byte) {
	start := len(s.queue)
	if len(record) <= maxRecord {
		s.queue = append(grow(s.queue, len(record)), record...)
	} else {
		s.queue = appendTruncated(grow(s.queue, maxRecord), record)
	}
	s.accepted += uint64(len(s.queue) - start)
	s.ends = append(s.ends, len(s.queue))
}

func grow(b []byte, n int) []byte {
	if cap(b)-len(b) >= n {
		return b
	}
	size := min(max(2*cap(b), len(b)+n, minCapacity), max(queueLimit, len(b)+n))
	grown := make([]byte, len(b), size)
	copy(grown, b)
	return grown
}

func appendTruncated(dst, record []byte) []byte {
	tail := truncatedMarker
	cut := maxRecord - len(tail)
	if bytes.IndexByte(record[:cut], 0x1b) >= 0 {
		tail = colorReset + truncatedMarker
		cut -= len(colorReset)
	}
	for cut > 0 && !utf8.RuneStart(record[cut]) {
		cut--
	}
	dst = append(dst, record[:cut]...)
	return append(dst, tail...)
}

func (s *Sink) drain() {
	s.mu.Lock()
	s.drainLocked()
}

func (s *Sink) drainLocked() {
	passed := false
	for {
		if len(s.queue) == 0 {
			if s.dropSeq != s.takenSeq {
				s.reportLocked()
				continue
			}
			s.writing = false
			if s.armed {
				s.armed = false
				s.timer.Stop()
			}
			s.releaseLocked()
			s.signalLocked()
			s.mu.Unlock()
			return
		}
		if passed && s.async > 0 && s.flushing == 0 && len(s.queue) < wakeSize {
			s.writing = false
			s.armLocked()
			if cap(s.spare) > keepCapacity {
				s.spare, s.spareEnds = nil, nil
			}
			s.mu.Unlock()
			return
		}
		passed = true
		buf, ends, out := s.queue, s.ends, s.out
		s.queue, s.ends = s.spare[:0], s.spareEnds[:0]
		s.spare, s.spareEnds = nil, nil
		s.mu.Unlock()
		start := 0
		for i := 0; i < len(ends); {
			j := i + 1
			for j < len(ends) && ends[j]-start <= batchSize {
				j++
			}
			end := ends[j-1]
			write(out, buf[start:end])
			s.mu.Lock()
			s.writes++
			s.batchDoneLocked(uint64(end - start))
			start, i = end, j
			if i < len(ends) {
				s.mu.Unlock()
			}
		}
		if s.spare == nil {
			s.spare, s.spareEnds = buf[:0], ends[:0]
		}
	}
}

func write(out io.Writer, p []byte) {
	defer func() { _ = recover() }()
	_, _ = out.Write(p)
}

func (s *Sink) batchDoneLocked(n uint64) {
	s.done += n
	s.stuck = false
	if s.reportPending && s.done >= s.pendingPos {
		s.reportPending = false
		s.reportedSeq = s.pendingSeq
	}
	s.signalLocked()
	if s.dropSeq != s.takenSeq && !s.reportPending && s.now().Sub(s.lastReport) >= reportInterval {
		s.reportLocked()
	}
}

func (s *Sink) reportLocked() {
	counts, seq, report := s.dropped, s.dropSeq, s.report
	s.dropped = [4]uint64{}
	s.takenSeq = seq
	s.lastReport = s.now()
	s.mu.Unlock()
	record := report(counts)
	s.mu.Lock()
	if len(record) == 0 {
		s.reportedSeq = seq
		s.signalLocked()
		return
	}
	s.appendLocked(record)
	s.reportPending = true
	s.pendingSeq = seq
	s.pendingPos = s.accepted
}

func (s *Sink) releaseLocked() {
	if cap(s.queue) > keepCapacity {
		s.queue, s.ends = nil, nil
	}
	if cap(s.spare) > keepCapacity {
		s.spare, s.spareEnds = nil, nil
	}
}

func (s *Sink) Snapshot() inspect.Snapshot {
	s.mu.Lock()
	fields := []inspect.Field{
		{Key: "wake_delay", Value: wakeDelay},
		{Key: "wake_size", Value: wakeSize},
		{Key: "batch_size", Value: batchSize},
		{Key: "queue_limit", Value: queueLimit},
		{Key: "soft_limit", Value: softLimit},
		{Key: "async", Value: s.async},
		{Key: "queued", Value: int64(s.accepted - s.done)},
		{Key: "writing", Value: s.writing},
		{Key: "timer_armed", Value: s.armed},
		{Key: "wakes", Value: int64(s.wakes)},
		{Key: "writes", Value: int64(s.writes)},
		{Key: "dropped_debug", Value: int64(s.dropped[Debug])},
		{Key: "dropped_info", Value: int64(s.dropped[Info])},
		{Key: "dropped_warn", Value: int64(s.dropped[Warn])},
		{Key: "dropped_error", Value: int64(s.dropped[Error])},
	}
	s.mu.Unlock()
	return inspect.Snapshot{Name: "logpipe", Fields: fields}
}
