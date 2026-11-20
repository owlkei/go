package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/test"
)

func lfFast() *RetryOptions {
	return &RetryOptions{InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}
}

func TestRetry_FirstSuccess(t *testing.T) {
	var calls int
	err := Retry(context.Background(), lfFast(), func(context.Context) error { calls++; return nil })

	if err != nil || calls != 1 {
		t.Error(test.DiffMessage([]any{err, calls}, []any{nil, 1}, "R1: a nil error returns nil at once"))
	}
}

func TestRetry_SucceedsAfterTransientErrors(t *testing.T) {
	var calls int
	err := Retry(context.Background(), lfFast(), func(context.Context) error {
		calls++
		if calls < 4 {
			return errors.New("lf-transient")
		}
		return nil
	})

	if err != nil || calls != 4 {
		t.Error(test.DiffMessage([]any{err, calls}, []any{nil, 4}, "R1: it calls again after each failure"))
	}
}

func TestRetry_PassesItsContextToFn(t *testing.T) {
	type key struct{}
	c := context.WithValue(context.Background(), key{}, "v")
	var got any
	_ = Retry(c, lfFast(), func(c context.Context) error { got = c.Value(key{}); return nil })

	if got != "v" {
		t.Error(test.DiffMessage(got, "v", "R1: fn receives a context derived from ctx"))
	}
}

func TestRetry_StopsOnCancel(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	last := errors.New("lf-last")
	var calls int
	err := Retry(c, lfFast(), func(context.Context) error {
		calls++
		if calls == 2 {
			cancel()
		}
		return last
	})

	if err == nil {
		t.Fatal(test.DiffMessage(nil, "an error", "R3: stopped"))
	}
	if !errors.Is(err, context.Canceled) || !errors.Is(err, last) {
		t.Error(test.DiffMessage(err, "Is context.Canceled and Is the last error", "R3: errors.Is finds both"))
	}
	if want := "owlkei: retry stopped after 2 attempts: "; !strings.HasPrefix(err.Error(), want) {
		t.Error(test.DiffMessage(err.Error(), want+"…", "R3: message"))
	}
	if calls != 2 {
		t.Error(test.DiffMessage(calls, 2, "R3: no call after the cancel"))
	}
}

func TestRetry_CtxAlreadyDone(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	err := Retry(c, lfFast(), func(context.Context) error { calls++; return nil })

	if calls != 0 {
		t.Error(test.DiffMessage(calls, 0, "R3: fn is not called when ctx is already done"))
	}
	if err == nil || !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "owlkei: retry stopped after 0 attempts: ") {
		t.Error(test.DiffMessage(err, "owlkei: retry stopped after 0 attempts: context canceled", "R3"))
	}
}

func TestRetry_StopsDuringWait(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- Retry(c, &RetryOptions{InitialDelay: time.Hour, MaxDelay: time.Hour}, func(context.Context) error {
			select {
			case <-started:
			default:
				close(started)
			}
			return errors.New("lf-fail")
		})
	}()
	lfWait(t, started, "the first attempt")
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Error(test.DiffMessage(err, "context canceled", "R3: ctx done during a wait"))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Retry did not return after the cancel")
	}
}

func TestRetry_MaxAttempts(t *testing.T) {
	var calls int
	last := errors.New("lf-always")
	opts := lfFast()
	opts.MaxAttempts = 3
	err := Retry(context.Background(), opts, func(context.Context) error { calls++; return last })

	if calls != 3 {
		t.Error(test.DiffMessage(calls, 3, "R4: it stops after MaxAttempts"))
	}
	if err == nil || !errors.Is(err, last) || err.Error() != "owlkei: retry gave up after 3 attempts: lf-always" {
		t.Error(test.DiffMessage(err, "owlkei: retry gave up after 3 attempts: lf-always", "R4"))
	}
}

func TestRetry_MaxAttemptsOneDoesNotWait(t *testing.T) {
	err := Retry(context.Background(), &RetryOptions{InitialDelay: time.Hour, MaxAttempts: 1}, func(context.Context) error { return errors.New("x") })

	if err == nil || !strings.HasPrefix(err.Error(), "owlkei: retry gave up after 1 attempts: ") {
		t.Error(test.DiffMessage(err, "gave up after 1 attempts, without waiting", "R4: the last attempt does not wait"))
	}
}

func TestRetry_NilOptions(t *testing.T) {
	var calls int
	err := Retry(context.Background(), nil, func(context.Context) error { calls++; return nil })
	if err != nil || calls != 1 {
		t.Error(test.DiffMessage([]any{err, calls}, []any{nil, 1}, "R5: nil options mean the defaults"))
	}

	c, cancel := context.WithCancel(context.Background())
	err = Retry(c, nil, func(context.Context) error { cancel(); return errors.New("x") })
	if !errors.Is(err, context.Canceled) {
		t.Error(test.DiffMessage(err, "context canceled", "R5: defaults, stopped by ctx"))
	}
}

func TestRetry_NilFnPanics(t *testing.T) {
	text, panicked := createPanicText(func() { _ = Retry(context.Background(), nil, nil) })

	if !panicked || !strings.Contains(text, "invalid retry: Retry needs a function") {
		t.Error(test.DiffMessage(text, "invalid retry: Retry needs a function", "R5"))
	}
}

func TestRetry_PanicInFnIsNotRecovered(t *testing.T) {
	value, panicked := createPanicValue(func() {
		_ = Retry(context.Background(), lfFast(), func(context.Context) error { panic("lf-fn-panic") })
	})

	if !panicked || fmt.Sprint(value) != "lf-fn-panic" {
		t.Error(test.DiffMessage(value, "lf-fn-panic", "R5: a panic in fn reaches the caller"))
	}
}

func TestRetry_DelayBounds(t *testing.T) {
	opts := &RetryOptions{InitialDelay: 20 * time.Millisecond, MaxDelay: 40 * time.Millisecond, MaxAttempts: 4}
	var stamps []time.Time
	_ = Retry(context.Background(), opts, func(context.Context) error {
		stamps = append(stamps, time.Now())
		return errors.New("x")
	})

	if len(stamps) != 4 {
		t.Fatal(test.DiffMessage(len(stamps), 4, "R2: attempts"))
	}
	for i, d := range []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond} {
		if gap := stamps[i+1].Sub(stamps[i]); gap < d/2 {
			t.Error(test.DiffMessage(gap, ">= "+(d/2).String(), fmt.Sprintf("R2: the wait before attempt %d is at least d/2", i+2)))
		}
	}
}

func lfRetryEntry(t *testing.T, factory func(context.Context) (*Module, error)) (*App, *lfWatchLog, *Module) {
	t.Helper()
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	logger := newLFWatchLog()
	app := New(&AppOptions{Logger: logger, IsDevtool: true})
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())
	return app, logger, user
}

func TestRetry_ReportsAttemptsToEntry(t *testing.T) {
	var calls atomic.Int64
	factory := func(c context.Context) (*Module, error) {
		err := Retry(c, &RetryOptions{InitialDelay: 4 * time.Millisecond, MaxDelay: 10 * time.Millisecond}, func(context.Context) error {
			if calls.Add(1) <= 5 {
				return errors.New("lf-transient")
			}
			return nil
		})
		return lfNamed("storage", NewModuleBuilder().Providers(lfSQL{})), err
	}
	app, logger, user := lfRetryEntry(t, factory)

	lfWait(t, app.container.settled(user), "user")

	if got := lfStatus(app, user); got != moduleReady {
		t.Error(test.DiffMessage(got.String(), "ready", "R6: ready after transient errors"))
	}
	entries := logger.named("ModuleRetry")
	if len(entries) != 5 {
		t.Fatal(test.DiffMessage(len(entries), 5, "R6: one ModuleRetry per failed attempt"))
	}
	for i, e := range entries {
		if e.level != "warn" || fmt.Sprint(e.field("attempt")) != fmt.Sprint(i+1) ||
			!strings.Contains(lcText(e.field("error")), "lf-transient") || e.field("module") == nil {
			t.Error(test.DiffMessage(e, "Warn with module, attempt, error, next", fmt.Sprintf("R6: line %d", i+1)))
		}
		d := time.Duration(4*time.Millisecond) << i
		if d > 10*time.Millisecond {
			d = 10 * time.Millisecond
		}
		next, ok := e.field("next").(time.Duration)
		if !ok || next < d/2 || next > d {
			t.Error(test.DiffMessage(e.field("next"), "a duration within ["+(d/2).String()+", "+d.String()+"]", fmt.Sprintf("R2, R6: wait after attempt %d, capped by MaxDelay", i+1)))
		}
	}

	resp, _ := app.devtool.GetConfiguration(context.Background(), nil)
	most := 0
	for _, m := range resp.Modules {
		most = max(most, m.Attempts)
	}
	if most != 5 {
		t.Error(test.DiffMessage(most, 5, "R6: attempts in the devtool snapshot"))
	}
}

func TestRetry_ForeignCtxReportsNothing(t *testing.T) {
	var calls atomic.Int64
	factory := func(context.Context) (*Module, error) {
		err := Retry(context.Background(), lfFast(), func(context.Context) error {
			if calls.Add(1) < 3 {
				return errors.New("lf-transient")
			}
			return nil
		})
		return lfNamed("storage", NewModuleBuilder().Providers(lfSQL{})), err
	}
	app, logger, user := lfRetryEntry(t, factory)

	lfWait(t, app.container.settled(user), "user")

	if n := len(logger.named("ModuleRetry")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "R6: with another ctx, Retry only retries"))
	}
	resp, _ := app.devtool.GetConfiguration(context.Background(), nil)
	for _, m := range resp.Modules {
		if m.Attempts != 0 {
			t.Error(test.DiffMessage(m.Attempts, 0, "R6: no attempts reported for "+m.Name))
		}
	}
}

func TestRetry_StopsAtShutdown(t *testing.T) {
	result := make(chan error, 1)
	started := make(chan struct{})
	var once atomic.Bool
	factory := func(c context.Context) (*Module, error) {
		err := Retry(c, lfFast(), func(context.Context) error {
			if once.CompareAndSwap(false, true) {
				close(started)
			}
			return errors.New("lf-down")
		})
		result <- err
		return nil, err
	}
	app, _, _ := lfRetryEntry(t, factory)
	lfWait(t, started, "the first attempt")

	lfStop(t, app)

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Error(test.DiffMessage(err, "context canceled", "R3: shutdown cancels the factory's ctx and Retry stops"))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Retry did not stop at shutdown")
	}
}
