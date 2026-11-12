package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestResumeDetected(t *testing.T) {
	const interval = 30 * time.Second
	threshold := ResumeThreshold(interval)
	for _, tc := range []struct {
		name     string
		expected time.Duration
		actual   time.Duration
		interval time.Duration
		want     bool
	}{
		{"normal wait", 30 * time.Second, 30 * time.Second, interval, false},
		{"small jitter", 30 * time.Second, 40 * time.Second, interval, false},
		{"shorter than expected", 30 * time.Second, 20 * time.Second, interval, false},
		{"exactly at threshold", 30 * time.Second, 30*time.Second + threshold, interval, false},
		{"above threshold", 30 * time.Second, 30*time.Second + threshold + time.Nanosecond, interval, true},
		{"backoff wait not resumed", 2 * time.Second, 2*time.Second + time.Millisecond, interval, false},
		{"backoff with suspend", 2 * time.Second, 2*time.Second + threshold + time.Second, interval, true},
		{"custom small interval", 1 * time.Second, 9 * time.Second, time.Second, true},
		{"custom below threshold", 1 * time.Second, 8 * time.Second, time.Second, false},
		{"zero interval", 1 * time.Second, 100 * time.Second, 0, false},
		{"negative expected", -time.Second, 100 * time.Second, interval, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResumeDetected(tc.expected, tc.actual, tc.interval); got != tc.want {
				t.Fatalf("ResumeDetected(%s, %s, %s) = %v, want %v", tc.expected, tc.actual, tc.interval, got, tc.want)
			}
		})
	}
}

func TestResumeThresholdScalesWithInterval(t *testing.T) {
	if got, want := ResumeThreshold(30*time.Second), 65*time.Second; got != want {
		t.Fatalf("resumeThreshold = %s, want %s", got, want)
	}
	if got, want := ResumeThreshold(5*time.Second), 15*time.Second; got != want {
		t.Fatalf("resumeThreshold = %s, want %s", got, want)
	}
}

func TestNetworkChangeTracker(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	observe := func(tr *networkChangeTracker, network string, offset time.Duration) bool {
		t.Helper()
		_, changed := tr.observe(network, base.Add(offset))
		return changed
	}

	t.Run("first observation establishes baseline", func(t *testing.T) {
		tr := newNetworkChangeTracker(30 * time.Second)
		if observe(tr, "nyist-tec", 0) {
			t.Fatal("initial observation must not trigger a change")
		}
	})

	t.Run("repeated identical network never re-authenticates", func(t *testing.T) {
		tr := newNetworkChangeTracker(30 * time.Second)
		for i := 0; i < 10; i++ {
			if observe(tr, "nyist-tec", time.Duration(i)*time.Second) {
				t.Fatalf("identical observation %d triggered a change", i)
			}
		}
	})

	t.Run("changed network requires two observations and triggers once", func(t *testing.T) {
		tr := newNetworkChangeTracker(30 * time.Second)
		if observe(tr, "nyist-tec", 0) {
			t.Fatal("baseline triggered")
		}
		if observe(tr, "nyist-stu", time.Second) {
			t.Fatal("single observation must not trigger")
		}
		if !observe(tr, "nyist-stu", 2*time.Second) {
			t.Fatal("second consecutive observation should trigger")
		}
		for i := 3; i < 8; i++ {
			if observe(tr, "nyist-stu", time.Duration(i)*time.Second) {
				t.Fatalf("observation %d triggered a duplicate change", i)
			}
		}
	})

	t.Run("flapping never reaches two consecutive observations", func(t *testing.T) {
		tr := newNetworkChangeTracker(30 * time.Second)
		if observe(tr, "nyist-tec", 0) {
			t.Fatal("baseline triggered")
		}
		for i := 1; i <= 20; i++ {
			network := "nyist-stu"
			if i%2 == 0 {
				network = "nyist-tec"
			}
			if observe(tr, network, time.Duration(i)*time.Second) {
				t.Fatalf("flapping triggered at observation %d", i)
			}
		}
	})

	t.Run("detection failure breaks consecutiveness", func(t *testing.T) {
		tr := newNetworkChangeTracker(30 * time.Second)
		if observe(tr, "nyist-tec", 0) || observe(tr, "nyist-stu", time.Second) {
			t.Fatal("unexpected change before detection failure")
		}
		tr.miss()
		if observe(tr, "nyist-stu", 2*time.Second) {
			t.Fatal("observation after a failed detection must start a new run")
		}
		if !observe(tr, "nyist-stu", 3*time.Second) {
			t.Fatal("two consecutive observations after the failure should trigger")
		}
	})

	t.Run("minimum interval throttles forced rechecks", func(t *testing.T) {
		tr := newNetworkChangeTracker(30 * time.Second)
		if observe(tr, "nyist-tec", 0) {
			t.Fatal("baseline triggered")
		}
		if observe(tr, "nyist-stu", time.Second) || !observe(tr, "nyist-stu", 2*time.Second) {
			t.Fatal("first change was not confirmed exactly once")
		}
		// A second real change immediately afterwards is held back by the
		// cooldown instead of forcing another recheck within 30 seconds.
		if observe(tr, "", 3*time.Second) || observe(tr, "", 4*time.Second) {
			t.Fatal("change during cooldown must not trigger")
		}
		// Once the cooldown expires the pending change triggers exactly once.
		if !observe(tr, "", 33*time.Second) {
			t.Fatal("pending change did not trigger after cooldown")
		}
		if observe(tr, "", 34*time.Second) {
			t.Fatal("pending change triggered twice")
		}
	})
}

func TestRunEventsNotificationState(t *testing.T) {
	failures, recoveries := 0, 0
	events := runEvents{
		onFailure:   func(error) { failures++ },
		onRecovered: func() { recoveries++ },
	}
	events.success()
	if failures != 0 || recoveries != 0 {
		t.Fatal("success without a prior failure must not notify")
	}
	events.failure(errors.New("first"))
	events.failure(errors.New("second"))
	if failures != 1 || recoveries != 0 {
		t.Fatalf("failure episode notifications = %d/%d, want 1/0", failures, recoveries)
	}
	events.success()
	events.success()
	if failures != 1 || recoveries != 1 {
		t.Fatalf("recovery notifications = %d/%d, want 1/1", failures, recoveries)
	}
	events.failure(errors.New("new episode"))
	if failures != 2 || recoveries != 1 {
		t.Fatalf("second episode notifications = %d/%d, want 2/1", failures, recoveries)
	}

	// Nil callbacks must be safe.
	var nilEvents runEvents
	nilEvents.failure(errors.New("ignored"))
	nilEvents.success()
}

func newTestRunner(opts RunOptions, detect func(context.Context) (string, error), connect func(context.Context, Config) (Status, error), wait func(context.Context, time.Duration) (time.Duration, error)) *keepaliveRunner {
	return &keepaliveRunner{
		cfg:      testConfig(),
		opts:     opts,
		interval: keepaliveInterval,
		clock:    time.Now,
		wait:     wait,
		detect:   detect,
		connect:  connect,
		logf:     func(string, ...any) {},
	}
}

func TestWaitKeepaliveHonorsContextAndElapsed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitKeepalive(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v, want context.Canceled", err)
	}

	const delay = 20 * time.Millisecond
	actual, err := waitKeepalive(context.Background(), delay)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if actual < delay {
		t.Fatalf("observed elapsed %s shorter than requested %s", actual, delay)
	}
}

func TestKeepaliveResumeTriggersImmediateCheck(t *testing.T) {
	connects, waits := 0, 0
	var logs []string
	r := newTestRunner(RunOptions{},
		func(context.Context) (string, error) { return "nyist-tec", nil },
		func(context.Context, Config) (Status, error) {
			connects++
			return Status{Online: true}, nil
		},
		func(_ context.Context, d time.Duration) (time.Duration, error) {
			waits++
			if waits == 1 {
				return d + ResumeThreshold(keepaliveInterval) + time.Millisecond, nil
			}
			return 0, context.Canceled
		},
	)
	r.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	if err := r.run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want context.Canceled", err)
	}
	if connects != 2 || waits != 2 {
		t.Fatalf("after resume: connects=%d waits=%d, want 2/2 (immediate check before the next wait)", connects, waits)
	}
	if !containsLog(logs, "resume from sleep") {
		t.Fatalf("resume was not reported: %v", logs)
	}
}

func TestKeepaliveNormalWaitDoesNotReportResume(t *testing.T) {
	connects := 0
	var logs []string
	r := newTestRunner(RunOptions{},
		func(context.Context) (string, error) { return "nyist-tec", nil },
		func(context.Context, Config) (Status, error) {
			connects++
			return Status{Online: true}, nil
		},
		func(_ context.Context, d time.Duration) (time.Duration, error) {
			if connects >= 2 {
				return 0, context.Canceled
			}
			return d + ResumeThreshold(keepaliveInterval), nil
		},
	)
	r.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	if err := r.run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want context.Canceled", err)
	}
	if containsLog(logs, "resume from sleep") {
		t.Fatalf("wait at the threshold reported a resume: %v", logs)
	}
}

func TestKeepaliveNetworkChangeDebouncesAndResetsFailures(t *testing.T) {
	networks := []string{
		"nyist-tec", "nyist-tec", "nyist-stu", "nyist-stu",
		"nyist-stu", "nyist-stu", "nyist-stu", "nyist-stu",
	}
	detects, connects := 0, 0
	connectErr := errors.New("portal unavailable (network or TLS error)")
	var logs []string
	r := newTestRunner(RunOptions{},
		func(context.Context) (string, error) {
			network := networks[detects]
			detects++
			return network, nil
		},
		func(context.Context, Config) (Status, error) {
			connects++
			return Status{}, connectErr
		},
		func(context.Context, time.Duration) (time.Duration, error) { return 0, nil },
	)
	r.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	if err := r.run(context.Background()); err == nil || err.Error() != connectErr.Error() {
		t.Fatalf("run = %v, want the fifth consecutive failure", err)
	}
	// Without the network-change reset the loop would have stopped after five
	// failures. The confirmed change on iteration four resets the budget, so
	// the loop keeps running and later exhausts a fresh budget.
	if connects != 8 || detects != 8 {
		t.Fatalf("connects=%d detects=%d, want 8/8", connects, detects)
	}
	changes := 0
	for _, line := range logs {
		if strings.Contains(line, "network changed") {
			changes++
			if !strings.Contains(line, `from "nyist-tec" to "nyist-stu"`) {
				t.Fatalf("unexpected change log: %s", line)
			}
		}
	}
	if changes != 1 {
		t.Fatalf("network change logged %d times, want exactly 1: %v", changes, logs)
	}
}

func TestKeepaliveDetectionErrorContinuesAndLogsOnce(t *testing.T) {
	detectErr := errors.New("cannot inspect Wi-Fi")
	connects, waits := 0, 0
	var logs []string
	r := newTestRunner(RunOptions{},
		func(context.Context) (string, error) { return "", detectErr },
		func(context.Context, Config) (Status, error) {
			connects++
			return Status{Online: true}, nil
		},
		func(context.Context, time.Duration) (time.Duration, error) {
			waits++
			if waits == 3 {
				return 0, context.Canceled
			}
			return 0, nil
		},
	)
	r.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	if err := r.run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want context.Canceled", err)
	}
	if connects != 3 {
		t.Fatalf("connects = %d, want 3: detection errors must not stop keepalive", connects)
	}
	detectionLogs := 0
	for _, line := range logs {
		if strings.Contains(line, "cannot detect network change") {
			detectionLogs++
		}
	}
	if detectionLogs != 1 {
		t.Fatalf("detection error logged %d times, want 1: %v", detectionLogs, logs)
	}
}

func TestKeepaliveNotificationCallbacks(t *testing.T) {
	t.Run("terminal authentication rejection notifies once and returns", func(t *testing.T) {
		failures, recoveries := 0, 0
		connects := 0
		r := newTestRunner(RunOptions{
			OnFailure:   func(error) { failures++ },
			OnRecovered: func() { recoveries++ },
		},
			func(context.Context) (string, error) { return "nyist-tec", nil },
			func(context.Context, Config) (Status, error) {
				connects++
				return Status{}, ErrCredentials
			},
			func(context.Context, time.Duration) (time.Duration, error) {
				t.Fatal("terminal failure must not sleep")
				return 0, nil
			},
		)
		if err := r.run(context.Background()); !errors.Is(err, ErrCredentials) {
			t.Fatalf("run = %v, want ErrCredentials", err)
		}
		if connects != 1 || failures != 1 || recoveries != 0 {
			t.Fatalf("connects=%d failures=%d recoveries=%d, want 1/1/0", connects, failures, recoveries)
		}
	})

	t.Run("context cancellation does not notify failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		failures := 0
		r := newTestRunner(RunOptions{OnFailure: func(error) { failures++ }},
			func(context.Context) (string, error) { return "nyist-tec", nil },
			func(context.Context, Config) (Status, error) {
				cancel()
				return Status{}, context.Canceled
			},
			func(context.Context, time.Duration) (time.Duration, error) {
				t.Fatal("cancelled connect must not sleep")
				return 0, nil
			},
		)
		if err := r.run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("run = %v, want context.Canceled", err)
		}
		if failures != 0 {
			t.Fatalf("cancellation fired %d failure notifications, want 0", failures)
		}
	})

	t.Run("failure then recovery fires once each", func(t *testing.T) {
		failures, recoveries := 0, 0
		connects, waits := 0, 0
		r := newTestRunner(RunOptions{
			OnFailure:   func(error) { failures++ },
			OnRecovered: func() { recoveries++ },
		},
			func(context.Context) (string, error) { return "nyist-tec", nil },
			func(context.Context, Config) (Status, error) {
				connects++
				if connects <= 2 {
					return Status{}, errors.New("portal unavailable (network or TLS error)")
				}
				return Status{Online: true}, nil
			},
			func(context.Context, time.Duration) (time.Duration, error) {
				waits++
				if waits == 3 {
					return 0, context.Canceled
				}
				return 0, nil
			},
		)
		if err := r.run(context.Background()); !errors.Is(err, context.Canceled) {
			t.Fatalf("run = %v, want context.Canceled", err)
		}
		if failures != 1 || recoveries != 1 {
			t.Fatalf("callbacks = %d failures/%d recoveries, want 1/1", failures, recoveries)
		}
	})

	t.Run("success without an event never notifies", func(t *testing.T) {
		failures, recoveries := 0, 0
		r := newTestRunner(RunOptions{
			OnFailure:   func(error) { failures++ },
			OnRecovered: func() { recoveries++ },
		},
			func(context.Context) (string, error) { return "nyist-tec", nil },
			func(context.Context, Config) (Status, error) { return Status{Online: true}, nil },
			func(context.Context, time.Duration) (time.Duration, error) { return 0, context.Canceled },
		)
		if err := r.run(context.Background()); !errors.Is(err, context.Canceled) {
			t.Fatalf("run = %v, want context.Canceled", err)
		}
		if failures != 0 || recoveries != 0 {
			t.Fatalf("callbacks = %d/%d, want 0/0", failures, recoveries)
		}
	})
}

func TestKeepaliveContextCanceledBeforeFirstCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	r := newTestRunner(RunOptions{},
		func(context.Context) (string, error) { calls++; return "nyist-tec", nil },
		func(context.Context, Config) (Status, error) { calls++; return Status{}, nil },
		func(context.Context, time.Duration) (time.Duration, error) { calls++; return 0, nil },
	)
	if err := r.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("cancelled run performed %d operations, want 0", calls)
	}
}

func TestRunWithOptionsRequiresCredentials(t *testing.T) {
	cfg := testConfig()
	cfg.Password = ""
	if err := RunWithOptions(context.Background(), cfg, nil, RunOptions{}); err == nil {
		t.Fatal("missing password accepted")
	}
}

func TestRunWithOptionsCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunWithOptions(ctx, testConfig(), nil, RunOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWithOptions = %v, want context.Canceled", err)
	}
}

func containsLog(logs []string, fragment string) bool {
	for _, line := range logs {
		if strings.Contains(line, fragment) {
			return true
		}
	}
	return false
}
