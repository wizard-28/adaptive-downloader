// Package network applies controlled Linux traffic-control (tc/netem)
// conditions for experiments.
//
// It shells out to the system `tc` command; it does not reimplement traffic
// control. It is independent of the downloader, policy, tracker, fetcher, and
// assembler.
//
// IMPORTANT: this package shapes a real network interface in the current
// network namespace. The development container shares the host network
// namespace, so shaping an interface (especially eth0) affects other traffic
// using that interface. Prefer loopback ("lo") for local httptest experiments,
// and verify which interface actually carries the experiment traffic.
package network

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	// ErrInvalidConfig indicates an unusable Config.
	ErrInvalidConfig = errors.New("network: invalid configuration")
	// ErrEmptyConfig indicates a Config with no condition to apply.
	ErrEmptyConfig = errors.New("network: empty configuration")
	// ErrInvalidInterface indicates an unusable interface name.
	ErrInvalidInterface = errors.New("network: invalid interface")
	// ErrTCUnavailable indicates the tc command could not be found.
	ErrTCUnavailable = errors.New("network: tc command unavailable")
	// ErrPermission indicates tc failed due to insufficient privileges.
	ErrPermission = errors.New("network: insufficient privileges")
	// ErrNoQdisc indicates there was no qdisc to remove.
	ErrNoQdisc = errors.New("network: no qdisc to remove")
)

// Config describes a netem condition. The zero Config applies nothing and is
// rejected by Validate. This is network-level shaping, distinct from the
// experiment server's application-level throttle.
type Config struct {
	// BandwidthBps limits the interface rate in bits per second (tc/netem
	// "rate"). Zero means no rate limit.
	BandwidthBps int64
	// Latency is the added one-way delay.
	Latency time.Duration
	// Jitter is the delay variation; it requires Latency > 0.
	Jitter time.Duration
	// LossPercent is the packet loss percentage in [0, 100].
	LossPercent float64
}

// IsZero reports whether the configuration requests no shaping.
func (c Config) IsZero() bool {
	return c.BandwidthBps == 0 && c.Latency == 0 && c.Jitter == 0 && c.LossPercent == 0
}

// Validate checks the configuration.
func (c Config) Validate() error {
	switch {
	case c.BandwidthBps < 0:
		return fmt.Errorf("%w: bandwidth must be >= 0, got %d", ErrInvalidConfig, c.BandwidthBps)
	case c.Latency < 0:
		return fmt.Errorf("%w: latency must be >= 0, got %v", ErrInvalidConfig, c.Latency)
	case c.Jitter < 0:
		return fmt.Errorf("%w: jitter must be >= 0, got %v", ErrInvalidConfig, c.Jitter)
	case c.Jitter > 0 && c.Latency <= 0:
		return fmt.Errorf("%w: jitter requires latency > 0", ErrInvalidConfig)
	case c.LossPercent < 0 || c.LossPercent > 100:
		return fmt.Errorf("%w: loss percent must be in [0,100], got %v", ErrInvalidConfig, c.LossPercent)
	case c.IsZero():
		return fmt.Errorf("%w: no bandwidth, latency, jitter, or loss set", ErrEmptyConfig)
	}
	return nil
}

// Controller runs tc (and ip) commands. It is safe for concurrent use.
type Controller struct {
	// Prefix is prepended to every command, e.g. []string{"sudo", "-n"}.
	Prefix []string
	// Exec overrides command execution (used in tests). When nil, the system
	// command is used.
	Exec func(name string, args ...string) ([]byte, error)
}

// NewController returns a Controller that runs the system tc command directly.
func NewController() *Controller { return &Controller{} }

func (c *Controller) exec(name string, args ...string) ([]byte, error) {
	fullName := name
	fullArgs := args
	if len(c.Prefix) > 0 {
		fullName = c.Prefix[0]
		fullArgs = append(append([]string{}, c.Prefix[1:]...), name)
		fullArgs = append(fullArgs, args...)
	}
	if c.Exec != nil {
		return c.Exec(fullName, fullArgs...)
	}
	return exec.Command(fullName, fullArgs...).CombinedOutput()
}

// Apply replaces the root qdisc on iface with a netem configuration. It never
// runs unless the caller explicitly invokes it.
//
// The command is exactly
//
//	tc qdisc replace dev <iface> root netem <delay D [J]> [loss P%] [rate R]
//
// "replace" makes it idempotent: applying twice leaves one qdisc, not two. The
// call reports only what tc reported; it does not verify that shaping took
// effect, so a caller that depends on the condition must check Show (or measure)
// and record a failure instead of assuming success.
func (c *Controller) Apply(iface string, cfg Config) error {
	if err := validateInterface(iface); err != nil {
		return err
	}
	opts, err := netemOptions(cfg)
	if err != nil {
		return err
	}
	args := append([]string{"qdisc", "replace", "dev", iface, "root", "netem"}, opts...)
	out, err := c.exec("tc", args...)
	if err != nil {
		return applyError(iface, err, out)
	}
	return nil
}

// Clear removes the root qdisc on iface. Removing a qdisc that does not exist
// returns an error wrapping ErrNoQdisc.
//
// That case is not a failure of cleanup: an interface with no root qdisc is
// already in the desired state. Experiment harnesses therefore treat ErrNoQdisc
// as success and any other error as "shaping may still be in place" (report it,
// do not hide it). Clear is safe to call twice.
func (c *Controller) Clear(iface string) error {
	if err := validateInterface(iface); err != nil {
		return err
	}
	out, err := c.exec("tc", "qdisc", "del", "dev", iface, "root")
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("%w: %s", ErrTCUnavailable, msg)
	case strings.Contains(msg, "No such file or directory"), strings.Contains(msg, "Cannot delete qdisc"):
		return fmt.Errorf("%w: %s on %q", ErrNoQdisc, msg, iface)
	case isPermission(msg):
		return fmt.Errorf("%w: clear qdisc on %q: %s", ErrPermission, iface, msg)
	default:
		return fmt.Errorf("network: clear qdisc on %q: %w: %s", iface, err, msg)
	}
}

// Show returns `tc qdisc show dev <interface>` output for diagnostics.
func (c *Controller) Show(iface string) (string, error) {
	if err := validateInterface(iface); err != nil {
		return "", err
	}
	out, err := c.exec("tc", "qdisc", "show", "dev", iface)
	if err != nil {
		return "", fmt.Errorf("network: show qdisc on %q: %w: %s", iface, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// InterfaceForHost resolves the interface used to reach host (host or
// host:port). Loopback addresses map to "lo" without invoking ip; other hosts
// are resolved with `ip route get`.
func (c *Controller) InterfaceForHost(host string) (string, error) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "lo", nil
	}
	out, err := c.exec("ip", "route", "get", host)
	if err != nil {
		return "", fmt.Errorf("network: resolve interface for %q: %w: %s", host, err, strings.TrimSpace(string(out)))
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("%w: cannot resolve interface for %q", ErrInvalidInterface, host)
}

// InterfaceExists reports whether iface exists in the current namespace.
func InterfaceExists(iface string) bool {
	if iface == "" {
		return false
	}
	_, err := os.Stat(filepath.Join("/sys/class/net", iface))
	return err == nil
}

// netemOptions builds the netem option list, e.g.
// ["delay","50ms","10ms","loss","2%","rate","1mbit"].
func netemOptions(cfg Config) ([]string, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var opts []string
	if cfg.Latency > 0 {
		opts = append(opts, "delay", formatDuration(cfg.Latency))
		if cfg.Jitter > 0 {
			opts = append(opts, formatDuration(cfg.Jitter))
		}
	}
	if cfg.LossPercent > 0 {
		opts = append(opts, "loss", fmt.Sprintf("%g%%", cfg.LossPercent))
	}
	if cfg.BandwidthBps > 0 {
		opts = append(opts, "rate", formatRate(cfg.BandwidthBps))
	}
	return opts, nil
}

func validateInterface(iface string) error {
	if iface == "" || strings.ContainsAny(iface, " \t\n") {
		return fmt.Errorf("%w: %q", ErrInvalidInterface, iface)
	}
	return nil
}

func applyError(iface string, err error, out []byte) error {
	msg := strings.TrimSpace(string(out))
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("%w: %s", ErrTCUnavailable, msg)
	case isPermission(msg):
		return fmt.Errorf("%w: apply netem on %q: %s", ErrPermission, iface, msg)
	default:
		return fmt.Errorf("network: apply netem on %q: %w: %s", iface, err, msg)
	}
}

func isPermission(msg string) bool {
	return strings.Contains(msg, "Operation not permitted") || strings.Contains(msg, "Permission denied")
}

func formatDuration(d time.Duration) string {
	switch {
	case d%time.Millisecond == 0:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d%time.Microsecond == 0:
		return fmt.Sprintf("%dus", d.Microseconds())
	default:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
}

func formatRate(bps int64) string {
	switch {
	case bps%1_000_000_000 == 0:
		return fmt.Sprintf("%dgbit", bps/1_000_000_000)
	case bps%1_000_000 == 0:
		return fmt.Sprintf("%dmbit", bps/1_000_000)
	case bps%1_000 == 0:
		return fmt.Sprintf("%dkbit", bps/1_000)
	default:
		return fmt.Sprintf("%dbit", bps)
	}
}

// Package-level helpers use a default Controller.
var defaultController = NewController()

// Apply applies cfg to iface using the default Controller.
func Apply(iface string, cfg Config) error { return defaultController.Apply(iface, cfg) }

// Clear removes shaping from iface using the default Controller.
func Clear(iface string) error { return defaultController.Clear(iface) }

// Show returns qdisc state for iface using the default Controller.
func Show(iface string) (string, error) { return defaultController.Show(iface) }
