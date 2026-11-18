package core

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/dangduoc08/ginject/internal/color"
)

const (
	defaultRetryInitialDelay = 500 * time.Millisecond
	defaultRetryMaxDelay     = 30 * time.Second
)

type RetryOptions struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

func Retry(ctx context.Context, opts *RetryOptions, fn func(ctx context.Context) error) error {
	if fn == nil {
		panic(color.FmtRed("invalid retry: Retry needs a function"))
	}
	o := loadRetryOptions(opts)
	e, _ := ctx.Value(entryKey{}).(*asyncEntry)

	var lastErr error
	var timer *time.Timer
	for n := 0; ; {
		if err := ctx.Err(); err != nil {
			return retryStopped(n, err, lastErr)
		}
		n++
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if o.MaxAttempts > 0 && n >= o.MaxAttempts {
			e.reportAttempt(n, err, 0)
			return fmt.Errorf("ginject: retry gave up after %d attempts: %w", n, lastErr)
		}
		wait := o.wait(n)
		e.reportAttempt(n, err, wait)
		if err := ctx.Err(); err != nil {
			e.clearNextRetry()
			return retryStopped(n, err, lastErr)
		}
		if timer == nil {
			timer = time.NewTimer(wait)
			defer timer.Stop()
		} else {
			timer.Reset(wait)
		}
		select {
		case <-timer.C:
			e.clearNextRetry()
		case <-ctx.Done():
			e.clearNextRetry()
			return retryStopped(n, ctx.Err(), lastErr)
		}
	}
}

func loadRetryOptions(opts *RetryOptions) RetryOptions {
	var o RetryOptions
	if opts != nil {
		o = *opts
	}
	if o.InitialDelay <= 0 {
		o.InitialDelay = defaultRetryInitialDelay
	}
	if o.MaxDelay <= 0 {
		o.MaxDelay = defaultRetryMaxDelay
	}
	return o
}

func (o RetryOptions) wait(n int) time.Duration {
	d := o.MaxDelay
	if shift := n - 1; shift < 62 && o.InitialDelay <= o.MaxDelay>>shift {
		d = o.InitialDelay << shift
	}
	half := d / 2
	return half + rand.N(d-half+1)
}

func retryStopped(n int, ctxErr, lastErr error) error {
	return fmt.Errorf("ginject: retry stopped after %d attempts: %w", n, errors.Join(ctxErr, lastErr))
}

func (e *asyncEntry) reportAttempt(n int, err error, next time.Duration) {
	if e == nil {
		return
	}
	e.attempts.Add(1)
	text := err.Error()
	e.lastError.Store(&text)
	if next > 0 {
		e.nextRetry.Store(time.Now().Add(next).UnixNano())
	}
	e.c.logger.Warn("ModuleRetry", "module", e.label(), "attempt", n, "error", err, "next", next)
}

func (e *asyncEntry) clearNextRetry() {
	if e != nil {
		e.nextRetry.Store(0)
	}
}
