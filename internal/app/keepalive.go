package app

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	// keepaliveInterval is the normal delay between keepalive checks.
	keepaliveInterval = 30 * time.Second
	// keepaliveMaxFailures is the number of consecutive transient failures
	// tolerated before Run returns. Authentication rejections and conflicting
	// accounts return immediately and do not use this budget.
	keepaliveMaxFailures = 5
	// resumeSlack is added to twice the normal interval when deciding whether a
	// measured wait was much longer than requested. A laptop suspend or a large
	// forward clock jump is only reported when it exceeds this extra slack, so
	// ordinary scheduler jitter and short stalls do not trigger a recheck.
	resumeSlack = 5 * time.Second
	// keepaliveWaitSlice bounds how long the waiter sleeps without sampling the
	// clock. Sleep hooks and monotonic timers do not necessarily advance while a
	// machine is suspended, so a long single timer could otherwise delay the
	// post-resume check until its original deadline. Slices only affect resume
	// detection latency; the total requested delay is unchanged.
	keepaliveWaitSlice = 5 * time.Second
	// networkChangeConfirmations is the number of consecutive observations of
	// the same new network required before treating it as a real change. This
	// keeps a transient scan result or a flapping association from triggering a
	// forced re-authentication.
	networkChangeConfirmations = 2
	// networkRecheckMinInterval throttles forced rechecks if the network keeps
	// changing. Normal keepalive checks are not throttled by this value.
	networkRecheckMinInterval = 30 * time.Second
)

// RunOptions configures optional keepalive notifications. The zero value keeps
// Run's historical behavior.
//
// Callbacks are invoked synchronously from the keepalive goroutine. Errors are
// already sanitized by the app package and never contain the password. A
// callback that panics will propagate and terminate the keepalive loop.
type RunOptions struct {
	// OnFailure is called once when a run of consecutive keepalive failures
	// starts. Further failures in the same run do not call it again. It is not
	// called for failures that coincide with context cancellation.
	OnFailure func(error)
	// OnRecovered is called once when a check succeeds after at least one
	// failure. It is never called after a success that was not preceded by a
	// failure.
	OnRecovered func()
}

// RunWithOptions is Run with optional failure/recovery notifications. It keeps
// the same account-protection semantics and returns the same errors as Run.
func RunWithOptions(ctx context.Context, cfg Config, report func(Status), opts RunOptions) error {
	if e := requireCredentials(cfg); e != nil {
		return e
	}
	r := &keepaliveRunner{
		cfg:      cfg,
		report:   report,
		opts:     opts,
		interval: keepaliveInterval,
		clock:    time.Now,
		wait:     waitKeepalive,
		detect:   DetectNetwork,
		connect:  Connect,
		logf:     keepaliveLogf,
	}
	return r.run(ctx)
}

// keepaliveLogf is a package variable so tests can capture operational logs
// without a logger dependency.
var keepaliveLogf = log.Printf

// ResumeThreshold returns how much longer than the requested wait is treated
// as evidence of a suspend or a large forward clock jump. It is exported so
// alternative keepalive implementations can share the same policy.
func ResumeThreshold(interval time.Duration) time.Duration {
	return 2*interval + resumeSlack
}

// ResumeDetected reports whether actual is long enough beyond expected to
// mean the process was suspended or starved. It is a pure helper so the policy
// can be unit-tested without suspending a machine. The threshold is strictly
// greater than 2*interval+resumeSlack.
func ResumeDetected(expected, actual, interval time.Duration) bool {
	if expected < 0 || actual <= expected || interval <= 0 {
		return false
	}
	return actual-expected > ResumeThreshold(interval)
}

// observedElapsed returns the wall-clock elapsed time when it exceeds the
// monotonic elapsed time, otherwise the monotonic value. Go's monotonic clock
// does not advance across suspend on every platform, while timers follow that
// same clock; comparing both readings lets a resume watchdog notice wall-clock
// time lost during suspension and large forward clock jumps. Backward clock
// jumps are ignored in favor of the monotonic reading.
func observedElapsed(start time.Time) time.Duration {
	mono := time.Since(start)
	wall := time.Now().Round(0).Sub(start.Round(0))
	if wall > mono {
		return wall
	}
	return mono
}

// waitKeepalive waits for d while remaining responsive to ctx. It sleeps in
// bounded slices so a resume can be noticed shortly after the process runs
// again even if the platform's monotonic timer was frozen during suspend. It
// returns the observed elapsed time, which the caller compares with the
// requested delay.
func waitKeepalive(ctx context.Context, d time.Duration) (time.Duration, error) {
	start := time.Now()
	deadline := start.Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return observedElapsed(start), nil
		}
		slice := remaining
		if slice > keepaliveWaitSlice {
			slice = keepaliveWaitSlice
		}
		timer := time.NewTimer(slice)
		select {
		case <-ctx.Done():
			timer.Stop()
			return observedElapsed(start), ctx.Err()
		case <-timer.C:
		}
		// Use wall time here: if the machine was suspended during the slice,
		// the full requested delay may already have elapsed in wall-clock terms
		// even though the monotonic timer has not fired yet.
		if observedElapsed(start) >= d {
			return observedElapsed(start), nil
		}
	}
}

// keepaliveRunner holds the loop's dependencies so behavior can be tested with
// a fake clock, waiter, detector and portal.
type keepaliveRunner struct {
	cfg      Config
	report   func(Status)
	opts     RunOptions
	interval time.Duration
	clock    func() time.Time
	wait     func(context.Context, time.Duration) (time.Duration, error)
	detect   func(context.Context) (string, error)
	connect  func(context.Context, Config) (Status, error)
	logf     func(string, ...any)
}

func (r *keepaliveRunner) run(ctx context.Context) error {
	failures := 0
	events := runEvents{onFailure: r.opts.OnFailure, onRecovered: r.opts.OnRecovered}
	tracker := newNetworkChangeTracker(networkRecheckMinInterval)
	detectionErrorLogged := false

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Network detection runs at most once per keepalive iteration. A
		// confirmed change resets the transient-failure budget and makes this
		// iteration reconnector rather than waiting for the backoff to expire.
		detected, err := r.detect(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if !detectionErrorLogged {
				r.logf("keepalive: cannot detect network change: %v; keeping previous settings", err)
				detectionErrorLogged = true
			}
			// A failed observation breaks consecutiveness: a candidate network
			// must be observed twice in a row on successful reads.
			tracker.miss()
		} else {
			detectionErrorLogged = false
			if previous, changed := tracker.observe(detected, r.clock()); changed {
				failures = 0
				r.logf("keepalive: network changed from %q to %q; rechecking immediately", previous, detected)
			}
		}

		s, err := r.connect(ctx, r.cfg)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			events.failure(err)
			if r.report != nil {
				r.report(Status{Message: err.Error()})
			}
			if errors.Is(err, ErrCredentials) || errors.Is(err, ErrOtherAccount) {
				return err
			}
			failures++
			if failures >= keepaliveMaxFailures {
				return err
			}
		} else {
			failures = 0
			events.success()
			if r.report != nil {
				r.report(s)
			}
		}

		delay := r.interval
		if failures > 0 {
			// 2/4/8/16 seconds for the first four transient failures.
			delay = time.Duration(1<<uint(failures)) * time.Second
		}
		actual, err := r.wait(ctx, delay)
		if err != nil {
			return err
		}
		if ResumeDetected(delay, actual, r.interval) {
			r.logf("keepalive: resume from sleep or clock jump detected (waited %s, expected %s); checking immediately", actual, delay)
		}
	}
}

// runEvents deduplicates failure/recovery notifications. A failure callback is
// emitted at most once per continuous failure episode, and a recovery callback
// only when a success actually ends such an episode.
type runEvents struct {
	onFailure   func(error)
	onRecovered func()
	failing     bool
}

func (e *runEvents) failure(err error) {
	if e.failing {
		return
	}
	e.failing = true
	if e.onFailure != nil {
		e.onFailure(err)
	}
}

func (e *runEvents) success() {
	if !e.failing {
		return
	}
	e.failing = false
	if e.onRecovered != nil {
		e.onRecovered()
	}
}

// networkChangeTracker is a debounced detector for the currently associated
// network. The first successful observation establishes the baseline; a new
// network must be observed on two consecutive checks, and forced rechecks are
// separated by at least minInterval. It is a pure state machine taking the
// observation time, so debounce behavior is unit-testable.
type networkChangeTracker struct {
	initialized   bool
	current       string
	candidate     string
	candidateSeen int
	lastAction    time.Time
	minInterval   time.Duration
}

func newNetworkChangeTracker(minInterval time.Duration) *networkChangeTracker {
	return &networkChangeTracker{minInterval: minInterval}
}

// miss records that a detection attempt failed, breaking the run of
// consecutive observations required to confirm a change.
func (t *networkChangeTracker) miss() {
	t.candidate = ""
	t.candidateSeen = 0
}

// observe records an observation at now and reports whether it confirms a new
// network that should trigger a forced recheck. previous is the network that
// was current before the change.
func (t *networkChangeTracker) observe(network string, now time.Time) (previous string, changed bool) {
	if !t.initialized {
		t.initialized = true
		t.current = network
		return "", false
	}
	if network == t.current {
		t.candidate = ""
		t.candidateSeen = 0
		return t.current, false
	}
	if network != t.candidate {
		t.candidate = network
		t.candidateSeen = 1
		return t.current, false
	}
	t.candidateSeen++
	if t.candidateSeen < networkChangeConfirmations {
		return t.current, false
	}
	if !t.lastAction.IsZero() && now.Sub(t.lastAction) < t.minInterval {
		// Keep the observation pending. It will trigger once the cooldown has
		// elapsed, still at most once for this network change.
		return t.current, false
	}
	previous = t.current
	t.current = network
	t.candidate = ""
	t.candidateSeen = 0
	t.lastAction = now
	return previous, true
}
