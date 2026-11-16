package accesslog

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/trace"
)

const (
	maxStagesPerRequest = 64
	maxPending          = 16384
	flushQueueSize      = 1024
	shardCount          = 64
	shardCapacity       = maxPending / shardCount
	inlineStages        = 8
	maxTargetBytes      = 2048
	stopTimeout         = 2 * time.Second
	dropReportInterval  = time.Second
)

type AccessLogConfig struct {
	Tracer *trace.Tracer
	Logger common.Logger
}

type AccessLog struct {
	logger     common.Logger
	stopListen func()
	shards     [shardCount]shard
	queue      chan flushJob
	quit       chan struct{}
	done       chan struct{}
	dropped    atomic.Int64
	stopped    atomic.Bool
	stopOnce   sync.Once
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

type flushJob struct {
	done trace.Event
	rec  *record
}

func NewAccessLog(cfg *AccessLogConfig) *AccessLog {
	if cfg == nil || cfg.Tracer == nil || cfg.Logger == nil {
		panic(color.FmtRed("invalid access log: a tracer and a logger are required"))
	}
	al := &AccessLog{
		logger: cfg.Logger,
		queue:  make(chan flushJob, flushQueueSize),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	for i := range al.shards {
		al.shards[i].byID = make(map[string]*record)
	}
	go al.worker()
	al.stopListen = cfg.Tracer.Listen(al.listen)
	return al
}

func (al *AccessLog) Stop() {
	al.stopOnce.Do(func() {
		al.stopped.Store(true)
		al.stopListen()
		close(al.quit)
		timer := time.NewTimer(stopTimeout)
		select {
		case <-al.done:
		case <-timer.C:
		}
		timer.Stop()
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
		select {
		case al.queue <- flushJob{done: e, rec: rec}:
		default:
			al.dropped.Add(1)
		}
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

func (al *AccessLog) worker() {
	defer close(al.done)
	ticker := time.NewTicker(dropReportInterval)
	defer ticker.Stop()
	for {
		select {
		case job := <-al.queue:
			al.write(job)
		case <-ticker.C:
			al.reportDropped()
		case <-al.quit:
			for {
				select {
				case job := <-al.queue:
					al.write(job)
				default:
					al.reportDropped()
					return
				}
			}
		}
	}
}

func (al *AccessLog) write(job flushJob) {
	defer func() {
		if recover() != nil {
			al.dropped.Add(1)
		}
	}()
	msg, args := lineArgs(job)
	al.logger.Info(msg, args...)
}

func (al *AccessLog) reportDropped() {
	n := al.dropped.Swap(0)
	if n == 0 {
		return
	}
	defer func() { _ = recover() }()
	al.logger.Warn("AccessLog", "dropped", n)
}

func lineArgs(job flushJob) (string, []any) {
	e := job.done
	var stages []stageEntry
	var stagesDropped int
	if job.rec != nil {
		stages = job.rec.stages
		stagesDropped = job.rec.dropped
	}

	var names [inlineStages]string
	order := names[:0]
	for _, s := range stages {
		if !containsString(order, s.stage) {
			order = append(order, s.stage)
		}
	}

	size := 10 + 2*len(order)
	if stagesDropped > 0 {
		size += 2
	}
	args := make([]any, 0, size)
	args = append(args,
		"id", e.ID,
		"transport", e.Transport,
		"code", e.Code,
		"operation", e.Operation,
	)

	if len(stages) > 0 {
		var ends [maxStagesPerRequest]int
		texts := stageTexts(stages, ends[:len(stages)])
		for _, name := range order {
			count := 0
			for _, s := range stages {
				if s.stage == name {
					count++
				}
			}
			list := make([]map[string]color.Colored, 0, count)
			for i, s := range stages {
				if s.stage != name {
					continue
				}
				start := 0
				if i > 0 {
					start = ends[i-1]
				}
				list = append(list, map[string]color.Colored{
					shortName(s.name): {Text: texts[start:ends[i]], Color: colorForDuration(s.duration)},
				})
			}
			args = append(args, name, list)
		}
	}
	if stagesDropped > 0 {
		args = append(args, "stages_dropped", stagesDropped)
	}
	args = append(args, "total", formatDuration(e.Duration))
	return cutTarget(e.Target), args
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func stageTexts(stages []stageEntry, ends []int) string {
	var percentBuf [maxStagesPerRequest]int
	percents := percentBuf[:len(stages)]
	distributePercentages(stages, percents)
	buf := make([]byte, 0, 24*len(stages))
	for i, s := range stages {
		buf = append(buf, formatDuration(s.duration)...)
		buf = append(buf, " ("...)
		buf = strconv.AppendInt(buf, int64(percents[i]), 10)
		buf = append(buf, "%)"...)
		ends[i] = len(buf)
	}
	return string(buf)
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
	switch {
	case d < time.Microsecond:
		return d.String()
	case d < time.Millisecond:
		return d.Round(time.Microsecond / 100).String()
	case d < time.Second:
		return d.Round(time.Millisecond / 100).String()
	default:
		return d.Round(time.Second / 100).String()
	}
}

func colorForDuration(d time.Duration) color.Color {
	switch {
	case d < 10*time.Millisecond:
		return color.Gray
	case d < 50*time.Millisecond:
		return color.Green
	case d < 100*time.Millisecond:
		return color.Blue
	case d < 300*time.Millisecond:
		return color.Yellow
	case d < 1000*time.Millisecond:
		return color.Orange
	default:
		return color.Red
	}
}

func shortName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
