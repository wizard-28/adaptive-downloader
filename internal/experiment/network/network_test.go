package network

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func integrationEnabled() bool {
	return os.Getenv("ADAPTIVEDL_NETWORK_TESTS") == "1"
}

type call struct {
	name string
	args []string
}

type fakeExec struct {
	calls []call
	out   []byte
	err   error
}

func (f *fakeExec) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	return f.out, f.err
}

func fakeController(f *fakeExec) *Controller {
	return &Controller{Exec: f.run}
}

func TestConfigValidate(t *testing.T) {
	valid := []Config{
		{BandwidthBps: 1_000_000},
		{Latency: 50 * time.Millisecond},
		{Latency: 50 * time.Millisecond, Jitter: 10 * time.Millisecond},
		{LossPercent: 2},
		{BandwidthBps: 8_000_000, Latency: 50 * time.Millisecond, Jitter: 5 * time.Millisecond, LossPercent: 1},
	}
	for i, cfg := range valid {
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid[%d] %+v: Validate = %v", i, cfg, err)
		}
	}

	invalid := []struct {
		name string
		cfg  Config
	}{
		{"empty", Config{}},
		{"negative bandwidth", Config{BandwidthBps: -1}},
		{"negative latency", Config{Latency: -time.Second}},
		{"negative jitter", Config{Latency: time.Second, Jitter: -time.Millisecond}},
		{"jitter without latency", Config{Jitter: 10 * time.Millisecond}},
		{"loss below zero", Config{LossPercent: -1}},
		{"loss above 100", Config{LossPercent: 101}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatalf("Validate = nil, want error")
			}
		})
	}
}

func TestApplyCommandConstruction(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			"bandwidth only",
			Config{BandwidthBps: 1_000_000},
			[]string{"qdisc", "replace", "dev", "lo", "root", "netem", "rate", "1mbit"},
		},
		{
			"bandwidth and latency",
			Config{BandwidthBps: 1_000_000, Latency: 50 * time.Millisecond},
			[]string{"qdisc", "replace", "dev", "lo", "root", "netem", "delay", "50ms", "rate", "1mbit"},
		},
		{
			"bandwidth and packet loss",
			Config{BandwidthBps: 1_000_000, LossPercent: 2},
			[]string{"qdisc", "replace", "dev", "lo", "root", "netem", "loss", "2%", "rate", "1mbit"},
		},
		{
			"bandwidth latency and jitter",
			Config{BandwidthBps: 8_000_000, Latency: 50 * time.Millisecond, Jitter: 10 * time.Millisecond},
			[]string{"qdisc", "replace", "dev", "lo", "root", "netem", "delay", "50ms", "10ms", "rate", "8mbit"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeExec{}
			if err := fakeController(f).Apply("lo", tc.cfg); err != nil {
				t.Fatalf("Apply = %v", err)
			}
			if len(f.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(f.calls))
			}
			if f.calls[0].name != "tc" {
				t.Errorf("command = %q, want tc", f.calls[0].name)
			}
			if !reflect.DeepEqual(f.calls[0].args, tc.want) {
				t.Errorf("args = %v, want %v", f.calls[0].args, tc.want)
			}
		})
	}
}

func TestApplyErrorHandling(t *testing.T) {
	t.Run("permission includes stderr", func(t *testing.T) {
		f := &fakeExec{out: []byte("RTNETLINK answers: Operation not permitted\n"), err: errors.New("exit status 2")}
		err := fakeController(f).Apply("eth0", Config{BandwidthBps: 1000})
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("err = %v, want ErrPermission", err)
		}
		if !strings.Contains(err.Error(), "Operation not permitted") {
			t.Fatalf("err does not include stderr: %v", err)
		}
	})
	t.Run("tc unavailable", func(t *testing.T) {
		f := &fakeExec{err: exec.ErrNotFound}
		err := fakeController(f).Apply("lo", Config{BandwidthBps: 1000})
		if !errors.Is(err, ErrTCUnavailable) {
			t.Fatalf("err = %v, want ErrTCUnavailable", err)
		}
	})
	t.Run("qdisc failure includes stderr", func(t *testing.T) {
		f := &fakeExec{out: []byte("RTNETLINK answers: File exists\n"), err: errors.New("exit status 2")}
		err := fakeController(f).Apply("lo", Config{BandwidthBps: 1000})
		if err == nil || !strings.Contains(err.Error(), "File exists") {
			t.Fatalf("err = %v, want stderr included", err)
		}
	})
}

func TestApplyInvalid(t *testing.T) {
	f := &fakeExec{}
	ctl := fakeController(f)

	if err := ctl.Apply("", Config{BandwidthBps: 1000}); !errors.Is(err, ErrInvalidInterface) {
		t.Fatalf("Apply empty iface = %v, want ErrInvalidInterface", err)
	}
	if err := ctl.Apply("bad iface", Config{BandwidthBps: 1000}); !errors.Is(err, ErrInvalidInterface) {
		t.Fatalf("Apply spaced iface = %v, want ErrInvalidInterface", err)
	}
	if err := ctl.Apply("lo", Config{}); !errors.Is(err, ErrEmptyConfig) {
		t.Fatalf("Apply empty cfg = %v, want ErrEmptyConfig", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("tc was invoked for invalid input: %+v", f.calls)
	}
}

func TestClear(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeExec{}
		if err := fakeController(f).Clear("lo"); err != nil {
			t.Fatalf("Clear = %v", err)
		}
		want := []string{"qdisc", "del", "dev", "lo", "root"}
		if f.calls[0].name != "tc" || !reflect.DeepEqual(f.calls[0].args, want) {
			t.Fatalf("call = %s %v, want tc %v", f.calls[0].name, f.calls[0].args, want)
		}
	})
	t.Run("no qdisc", func(t *testing.T) {
		f := &fakeExec{out: []byte("RTNETLINK answers: No such file or directory\n"), err: errors.New("exit status 2")}
		if err := fakeController(f).Clear("lo"); !errors.Is(err, ErrNoQdisc) {
			t.Fatalf("Clear = %v, want ErrNoQdisc", err)
		}
	})
}

func TestShow(t *testing.T) {
	f := &fakeExec{out: []byte("qdisc netem 8001: root refcnt 2 limit 1000 delay 50ms\n")}
	out, err := fakeController(f).Show("lo")
	if err != nil {
		t.Fatalf("Show = %v", err)
	}
	if !strings.Contains(out, "netem") {
		t.Fatalf("Show = %q, want netem", out)
	}
	want := []string{"qdisc", "show", "dev", "lo"}
	if !reflect.DeepEqual(f.calls[0].args, want) {
		t.Fatalf("args = %v, want %v", f.calls[0].args, want)
	}
}

func TestInterfaceForHost(t *testing.T) {
	t.Run("loopback does not invoke ip", func(t *testing.T) {
		f := &fakeExec{}
		ctl := fakeController(f)
		for _, host := range []string{"127.0.0.1", "127.0.0.1:8080", "::1", "[::1]:80"} {
			got, err := ctl.InterfaceForHost(host)
			if err != nil {
				t.Fatalf("InterfaceForHost(%q) = %v", host, err)
			}
			if got != "lo" {
				t.Fatalf("InterfaceForHost(%q) = %q, want lo", host, got)
			}
		}
		if len(f.calls) != 0 {
			t.Fatalf("ip was invoked for loopback: %+v", f.calls)
		}
	})
	t.Run("non-loopback parses ip route", func(t *testing.T) {
		f := &fakeExec{out: []byte("10.0.0.5 dev eth0 src 10.0.0.1 uid 0\n")}
		got, err := fakeController(f).InterfaceForHost("10.0.0.5")
		if err != nil {
			t.Fatalf("InterfaceForHost = %v", err)
		}
		if got != "eth0" {
			t.Fatalf("iface = %q, want eth0", got)
		}
		want := []string{"route", "get", "10.0.0.5"}
		if f.calls[0].name != "ip" || !reflect.DeepEqual(f.calls[0].args, want) {
			t.Fatalf("call = %s %v, want ip %v", f.calls[0].name, f.calls[0].args, want)
		}
	})
}

func TestInterfaceExists(t *testing.T) {
	if !InterfaceExists("lo") {
		t.Fatal("InterfaceExists(lo) = false, want true")
	}
	if InterfaceExists("definitely-not-a-real-interface-xyz") {
		t.Fatal("InterfaceExists(bogus) = true, want false")
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := formatRate(1_000_000); got != "1mbit" {
		t.Errorf("formatRate = %q", got)
	}
	if got := formatRate(1_000); got != "1kbit" {
		t.Errorf("formatRate = %q", got)
	}
	if got := formatRate(1234); got != "1234bit" {
		t.Errorf("formatRate = %q", got)
	}
	if got := formatDuration(50 * time.Millisecond); got != "50ms" {
		t.Errorf("formatDuration = %q", got)
	}
	if got := formatDuration(1500 * time.Microsecond); got != "1500us" {
		t.Errorf("formatDuration = %q", got)
	}
}

// TestIntegrationApplyClear applies a small netem condition to loopback. It is
// skipped unless ADAPTIVEDL_NETWORK_TESTS=1 and privileges are available, so
// the normal suite never depends on root.
func TestIntegrationApplyClear(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if !integrationEnabled() {
		t.Skip("set ADAPTIVEDL_NETWORK_TESTS=1 to run privileged tc/netem integration tests")
	}
	if _, err := exec.LookPath("tc"); err != nil {
		t.Skipf("tc not available: %v", err)
	}
	if !InterfaceExists("lo") {
		t.Skip("lo interface not available")
	}

	controllers := map[string]*Controller{
		"direct": NewController(),
	}
	if _, err := exec.LookPath("sudo"); err == nil {
		controllers["sudo"] = &Controller{Prefix: []string{"sudo", "-n"}}
	}

	ran := false
	for name, ctl := range controllers {
		t.Run(name, func(t *testing.T) {
			cfg := Config{BandwidthBps: 1_000_000, Latency: 5 * time.Millisecond}
			if err := ctl.Apply("lo", cfg); err != nil {
				if errors.Is(err, ErrPermission) || errors.Is(err, ErrTCUnavailable) {
					t.Skipf("cannot apply netem: %v", err)
				}
				t.Fatalf("Apply: %v", err)
			}
			defer func() { _ = ctl.Clear("lo") }()

			out, err := ctl.Show("lo")
			if err != nil {
				t.Fatalf("Show: %v", err)
			}
			if !strings.Contains(out, "netem") {
				t.Fatalf("qdisc show missing netem: %q", out)
			}
			if err := ctl.Clear("lo"); err != nil && !errors.Is(err, ErrNoQdisc) {
				t.Fatalf("Clear: %v", err)
			}
			ran = true
		})
	}
	if !ran {
		t.Skip("no privileged tc/netem execution available")
	}
}
