// Command live-pilot runs the descriptive live-internet pilot L-P1
// (testdata/live_pilot_l1/protocol.md): fixed (N,S) cells of the project
// downloader, aria2 and a single-stream curl reference against real public
// HTTPS mirrors, one time-of-day slot per invocation.
//
// Every run appends exactly one JSON line to the slot's output file, valid or
// not; nothing is discarded. Re-invoking with the same output resumes the slot
// by skipping (server, arm, rep) keys already recorded.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/fetcher"
)

// Manifest is the frozen pilot design (testdata/live_pilot_l1/manifest.json).
type Manifest struct {
	Pilot         string   `json:"pilot"`
	Seed          int64    `json:"seed"`
	File          File     `json:"file"`
	Servers       []Server `json:"servers"`
	Cells         []Cell   `json:"cells"`
	Aria2Args     []string `json:"aria2_args"`
	Reps          int      `json:"reps"`
	GapSec        int      `json:"gap_s"`
	RunTimeoutSec int      `json:"run_timeout_s"`
	MaxRetries    int      `json:"max_retries"`
	TimelineMs    int      `json:"timeline_ms"`
}

type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Server struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type Cell struct {
	N int   `json:"n"`
	S int64 `json:"s"`
}

// Arm is one measured configuration within a block.
type Arm struct {
	Kind string `json:"kind"` // "ours", "aria2", "curl"
	N    int    `json:"n,omitempty"`
	S    int64  `json:"s,omitempty"`
}

func (a Arm) Name() string {
	if a.Kind == "ours" {
		return fmt.Sprintf("ours_N%d_S%dM", a.N, a.S>>20)
	}
	return a.Kind
}

// Record is one run; exactly one is written per attempted run.
type Record struct {
	Pilot      string    `json:"pilot"`
	Slot       string    `json:"slot"`
	Server     string    `json:"server"`
	URL        string    `json:"url"`
	Arm        string    `json:"arm"`
	Kind       string    `json:"kind"`
	N          int       `json:"n,omitempty"`
	S          int64     `json:"s,omitempty"`
	Rep        int       `json:"rep"`
	Order      int       `json:"order"`
	StartUTC   time.Time `json:"start_utc"`
	DurationS  float64   `json:"duration_s"`
	Bytes      int64     `json:"bytes"`
	SHA256     string    `json:"sha256"`
	ChecksumOK bool      `json:"checksum_ok"`
	Valid      bool      `json:"valid"`
	Err        string    `json:"err,omitempty"`
	// Ours only.
	Attempts       int64          `json:"attempts,omitempty"`
	FailedAttempts int64          `json:"failed_attempts,omitempty"`
	StatusCodes    map[string]int `json:"status_codes,omitempty"`
	TimelineMs     int            `json:"timeline_ms,omitempty"`
	Timeline       []int64        `json:"timeline_bytes,omitempty"`
	// External tools.
	Command string `json:"command,omitempty"`
	Version string `json:"version,omitempty"`
	// RateLimited marks a 429/503 observation; the server is then skipped for
	// the rest of the slot (protocol etiquette rule).
	RateLimited bool `json:"rate_limited,omitempty"`
}

func (r Record) key() string { return fmt.Sprintf("%s|%s|%d", r.Server, r.Arm, r.Rep) }

func main() {
	manifestPath := flag.String("manifest", filepath.Join("testdata", "live_pilot_l1", "manifest.json"), "pilot manifest")
	slot := flag.String("slot", "", "time-of-day slot label (required), e.g. S1")
	out := flag.String("out", "", "JSONL output (default testdata/live_pilot_l1/raw/<slot>.jsonl)")
	only := flag.String("servers", "", "comma-separated server ids to run (default all)")
	reps := flag.Int("reps", 0, "override manifest reps (smoke runs only)")
	aria2 := flag.String("aria2", "aria2c", "aria2c binary")
	curl := flag.String("curl", "curl", "curl binary")
	flag.Parse()
	if *slot == "" {
		fatal(errors.New("-slot is required"))
	}
	m, err := loadManifest(*manifestPath)
	if err != nil {
		fatal(err)
	}
	if *reps > 0 {
		m.Reps = *reps
	}
	if *out == "" {
		*out = filepath.Join(filepath.Dir(*manifestPath), "raw", *slot+".jsonl")
	}
	servers := m.Servers
	if *only != "" {
		servers = filterServers(m.Servers, strings.Split(*only, ","))
	}
	done, err := loadDone(*out)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fatal(err)
	}
	defer f.Close()

	r := &runner{m: m, slot: *slot, aria2: *aria2, curl: *curl, aria2Version: toolVersion(*aria2, "--version"), curlVersion: toolVersion(*curl, "--version")}
	plan := Schedule(m, *slot, servers)
	limited := map[string]bool{}
	for i, p := range plan {
		k := fmt.Sprintf("%s|%s|%d", p.Server.ID, p.Arm.Name(), p.Rep)
		if done[k] || limited[p.Server.ID] {
			continue
		}
		rec := r.run(p, i)
		if rec.RateLimited {
			limited[p.Server.ID] = true
		}
		line, _ := json.Marshal(rec)
		if _, err := f.Write(append(line, '\n')); err != nil {
			fatal(err)
		}
		fmt.Printf("[%s %d/%d] %s %s rep%d valid=%v %.1fs %s\n", *slot, i+1, len(plan), p.Server.ID, p.Arm.Name(), p.Rep, rec.Valid, rec.DurationS, rec.Err)
		if i < len(plan)-1 && m.GapSec > 0 {
			time.Sleep(time.Duration(m.GapSec) * time.Second)
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "live-pilot:", err)
	os.Exit(1)
}

func loadManifest(path string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	return m, m.validate()
}

func (m Manifest) validate() error {
	switch {
	case m.File.Size <= 0:
		return errors.New("manifest: file.size must be positive")
	case len(m.File.SHA256) != 64:
		return errors.New("manifest: file.sha256 must be 64 hex chars")
	case len(m.Servers) == 0:
		return errors.New("manifest: no servers")
	case len(m.Cells) == 0:
		return errors.New("manifest: no cells")
	case m.Reps <= 0:
		return errors.New("manifest: reps must be positive")
	case m.RunTimeoutSec <= 0:
		return errors.New("manifest: run_timeout_s must be positive")
	case m.TimelineMs <= 0:
		return errors.New("manifest: timeline_ms must be positive")
	}
	seen := map[string]bool{}
	for _, s := range m.Servers {
		if s.ID == "" || s.URL == "" || seen[s.ID] {
			return fmt.Errorf("manifest: bad or duplicate server %q", s.ID)
		}
		seen[s.ID] = true
	}
	for _, c := range m.Cells {
		if c.N <= 0 || c.S <= 0 {
			return fmt.Errorf("manifest: bad cell %+v", c)
		}
	}
	return nil
}

func filterServers(all []Server, ids []string) []Server {
	want := map[string]bool{}
	for _, id := range ids {
		want[strings.TrimSpace(id)] = true
	}
	var out []Server
	for _, s := range all {
		if want[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// Planned is one scheduled run.
type Planned struct {
	Server Server
	Arm    Arm
	Rep    int
}

// Arms returns the manifest's arms in canonical order: the fixed cells, then
// aria2, then the curl single-stream reference.
func Arms(m Manifest) []Arm {
	var arms []Arm
	for _, c := range m.Cells {
		arms = append(arms, Arm{Kind: "ours", N: c.N, S: c.S})
	}
	return append(arms, Arm{Kind: "aria2"}, Arm{Kind: "curl"})
}

// Schedule is the deterministic run order for a slot: for each rep, each
// server forms a block (server order shuffled per rep); within a block the
// arms are shuffled. The RNG is seeded from the manifest seed and the slot
// label only, so the order is reproducible.
func Schedule(m Manifest, slot string, servers []Server) []Planned {
	h := fnv.New64a()
	_, _ = h.Write([]byte(slot))
	rng := rand.New(rand.NewSource(m.Seed ^ int64(h.Sum64())))
	var plan []Planned
	for rep := 0; rep < m.Reps; rep++ {
		srv := append([]Server(nil), servers...)
		rng.Shuffle(len(srv), func(i, j int) { srv[i], srv[j] = srv[j], srv[i] })
		for _, s := range srv {
			arms := Arms(m)
			rng.Shuffle(len(arms), func(i, j int) { arms[i], arms[j] = arms[j], arms[i] })
			for _, a := range arms {
				plan = append(plan, Planned{Server: s, Arm: a, Rep: rep})
			}
		}
	}
	return plan
}

func loadDone(path string) (map[string]bool, error) {
	done := map[string]bool{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return done, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s: corrupt line: %w", path, err)
		}
		done[r.key()] = true
	}
	return done, sc.Err()
}

type runner struct {
	m            Manifest
	slot         string
	aria2, curl  string
	aria2Version string
	curlVersion  string
}

func (r *runner) run(p Planned, order int) Record {
	rec := Record{
		Pilot: r.m.Pilot, Slot: r.slot, Server: p.Server.ID, URL: p.Server.URL,
		Arm: p.Arm.Name(), Kind: p.Arm.Kind, N: p.Arm.N, S: p.Arm.S, Rep: p.Rep, Order: order,
	}
	dir, err := os.MkdirTemp("", "live-pilot-")
	if err != nil {
		rec.Err = err.Error()
		return rec
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.m.RunTimeoutSec)*time.Second)
	defer cancel()

	rec.StartUTC = time.Now().UTC()
	start := time.Now()
	var runErr error
	switch p.Arm.Kind {
	case "ours":
		runErr = r.runOurs(ctx, p, outPath, start, &rec)
	case "aria2":
		runErr = r.runAria2(ctx, p, dir, &rec)
	case "curl":
		runErr = r.runCurl(ctx, p, outPath, &rec)
	default:
		runErr = fmt.Errorf("unknown arm kind %q", p.Arm.Kind)
	}
	rec.DurationS = time.Since(start).Seconds()
	if runErr != nil {
		rec.Err = runErr.Error()
	}
	finishChecksum(outPath, r.m.File, &rec)
	rec.Valid = runErr == nil && rec.ChecksumOK
	return rec
}

// finishChecksum hashes the output and records size and checksum validity.
func finishChecksum(path string, want File, rec *Record) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return
	}
	rec.Bytes = n
	rec.SHA256 = hex.EncodeToString(h.Sum(nil))
	rec.ChecksumOK = n == want.Size && strings.EqualFold(rec.SHA256, want.SHA256)
}

func (r *runner) runOurs(ctx context.Context, p Planned, outPath string, start time.Time, rec *Record) error {
	tl := NewTimeline(start, time.Duration(r.m.TimelineMs)*time.Millisecond)
	dl, err := downloader.New(downloader.Config{
		URL:              p.Server.URL,
		OutputPath:       outPath,
		TotalSize:        r.m.File.Size,
		Concurrency:      p.Arm.N,
		ChunkSize:        p.Arm.S,
		NoRequestTimeout: true, // watchdog-only: the run context is the deadline
		MaxRetries:       r.m.MaxRetries,
		ByteObserver:     tl,
	})
	if err != nil {
		return err
	}
	runErr := dl.Download(ctx)
	snap := dl.Metrics()
	rec.Attempts = snap.Aggregate.TotalAttempts
	rec.FailedAttempts = snap.Aggregate.FailedRequests
	rec.StatusCodes = map[string]int{}
	for _, rr := range snap.Records {
		rec.StatusCodes[strconv.Itoa(rr.StatusCode)]++
		if rr.StatusCode == 429 || rr.StatusCode == 503 {
			rec.RateLimited = true
		}
	}
	rec.TimelineMs = r.m.TimelineMs
	rec.Timeline = tl.Buckets()
	return runErr
}

func (r *runner) runAria2(ctx context.Context, p Planned, dir string, rec *Record) error {
	args := append([]string{}, r.m.Aria2Args...)
	args = append(args, "-d", dir, "-o", "out.bin", p.Server.URL)
	rec.Command = r.aria2 + " " + strings.Join(args, " ")
	rec.Version = r.aria2Version
	out, err := exec.CommandContext(ctx, r.aria2, args...).CombinedOutput()
	low := string(out)
	if strings.Contains(low, "status=429") || strings.Contains(low, "status=503") {
		rec.RateLimited = true
	}
	if err != nil {
		return fmt.Errorf("aria2: %v: %s", err, tail(string(out), 300))
	}
	return nil
}

func (r *runner) runCurl(ctx context.Context, p Planned, outPath string, rec *Record) error {
	args := []string{"-sS", "--fail", "-o", outPath, "-w", "%{http_code}", p.Server.URL}
	rec.Command = r.curl + " " + strings.Join(args, " ")
	rec.Version = r.curlVersion
	out, err := exec.CommandContext(ctx, r.curl, args...).CombinedOutput()
	code := strings.TrimSpace(string(out))
	if strings.HasSuffix(code, "429") || strings.HasSuffix(code, "503") {
		rec.RateLimited = true
	}
	if err != nil {
		return fmt.Errorf("curl: %v: %s", err, tail(code, 300))
	}
	return nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func toolVersion(bin, flag string) string {
	out, err := exec.Command(bin, flag).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// Timeline is a concurrency-safe fetcher.ByteObserver that sums received bytes
// into fixed-width buckets measured from the run start.
type Timeline struct {
	mu      sync.Mutex
	start   time.Time
	width   time.Duration
	buckets map[int]int64
	max     int
}

var _ fetcher.ByteObserver = (*Timeline)(nil)

func NewTimeline(start time.Time, width time.Duration) *Timeline {
	return &Timeline{start: start, width: width, buckets: map[int]int64{}, max: -1}
}

func (t *Timeline) ObserveBytes(e fetcher.ByteEvent) {
	i := int(e.Time.Sub(t.start) / t.width)
	if i < 0 {
		i = 0
	}
	t.mu.Lock()
	t.buckets[i] += e.Delta
	if i > t.max {
		t.max = i
	}
	t.mu.Unlock()
}

// Buckets returns a dense slice of per-bucket byte counts.
func (t *Timeline) Buckets() []int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]int64, t.max+1)
	keys := make([]int, 0, len(t.buckets))
	for k := range t.buckets {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		out[k] = t.buckets[k]
	}
	return out
}
