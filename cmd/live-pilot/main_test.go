package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"adaptivedl/internal/fetcher"
)

func testContent(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*31 + 7) % 256)
	}
	return b
}

func rangeServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(content))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func testManifest(url string, content []byte) Manifest {
	sum := sha256.Sum256(content)
	return Manifest{
		Pilot: "test", Seed: 7,
		File:    File{Name: "f.bin", Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])},
		Servers: []Server{{ID: "a", URL: url}, {ID: "b", URL: url}},
		Cells:   []Cell{{N: 1, S: 1 << 20}, {N: 4, S: 1 << 20}, {N: 4, S: 4 << 20}},
		Reps:    2, RunTimeoutSec: 30, MaxRetries: 1, TimelineMs: 100,
	}
}

func TestManifestValidate(t *testing.T) {
	m := testManifest("http://x", testContent(10))
	if err := m.validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := m
	bad.File.SHA256 = "abc"
	if bad.validate() == nil {
		t.Fatal("short sha256 accepted")
	}
	bad = m
	bad.Servers = []Server{{ID: "a", URL: "u"}, {ID: "a", URL: "u"}}
	if bad.validate() == nil {
		t.Fatal("duplicate server accepted")
	}
	bad = m
	bad.Cells = []Cell{{N: 0, S: 1}}
	if bad.validate() == nil {
		t.Fatal("bad cell accepted")
	}
}

func TestScheduleDeterministicAndComplete(t *testing.T) {
	m := testManifest("http://x", testContent(10))
	p1 := Schedule(m, "S1", m.Servers)
	p2 := Schedule(m, "S1", m.Servers)
	if !reflect.DeepEqual(p1, p2) {
		t.Fatal("schedule not deterministic for the same slot")
	}
	if reflect.DeepEqual(p1, Schedule(m, "S2", m.Servers)) {
		t.Fatal("different slots produced identical order")
	}
	arms := len(Arms(m))
	if arms != len(m.Cells)+2 {
		t.Fatalf("arms = %d, want cells+aria2+curl", arms)
	}
	if len(p1) != arms*len(m.Servers)*m.Reps {
		t.Fatalf("plan length %d", len(p1))
	}
	seen := map[string]bool{}
	for i, p := range p1 {
		k := p.Server.ID + p.Arm.Name() + string(rune('0'+p.Rep))
		if seen[k] {
			t.Fatalf("duplicate run %s", k)
		}
		seen[k] = true
		// Blocks: each consecutive group of `arms` runs is one (rep, server).
		first := p1[(i/arms)*arms]
		if p.Server.ID != first.Server.ID || p.Rep != first.Rep {
			t.Fatalf("run %d breaks block structure", i)
		}
	}
}

func TestRunOursChecksum(t *testing.T) {
	content := testContent(3<<20 + 12345)
	ts := rangeServer(t, content)
	m := testManifest(ts.URL, content)
	r := &runner{m: m, slot: "T"}
	rec := r.run(Planned{Server: m.Servers[0], Arm: Arm{Kind: "ours", N: 4, S: 1 << 20}}, 0)
	if !rec.Valid || !rec.ChecksumOK || rec.Bytes != int64(len(content)) {
		t.Fatalf("expected valid run, got %+v", rec)
	}
	var sum int64
	for _, b := range rec.Timeline {
		sum += b
	}
	if sum != int64(len(content)) {
		t.Fatalf("timeline sums to %d, want %d", sum, len(content))
	}
	if rec.Attempts < 4 || rec.StatusCodes["206"] != int(rec.Attempts) {
		t.Fatalf("unexpected attempt accounting: %d attempts, codes %v", rec.Attempts, rec.StatusCodes)
	}

	// A wrong expected checksum must produce an invalid, still-recorded run.
	m.File.SHA256 = hex.EncodeToString(make([]byte, 32))
	r = &runner{m: m, slot: "T"}
	rec = r.run(Planned{Server: m.Servers[0], Arm: Arm{Kind: "ours", N: 2, S: 1 << 20}}, 0)
	if rec.Valid || rec.ChecksumOK || rec.Bytes != int64(len(content)) {
		t.Fatalf("checksum mismatch not marked invalid: %+v", rec)
	}
}

func TestRunOursUserAgent(t *testing.T) {
	content := testContent(1 << 20)
	uas := make(chan string, 16)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uas <- r.Header.Get("User-Agent")
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(content))
	}))
	defer ts.Close()
	m := testManifest(ts.URL, content)
	m.UserAgent = "adaptivedl-research-pilot/1.0"
	rec := (&runner{m: m, slot: "T"}).run(Planned{Server: m.Servers[0], Arm: Arm{Kind: "ours", N: 1, S: 1 << 20}}, 0)
	if !rec.Valid || rec.UserAgent != m.UserAgent {
		t.Fatalf("run %+v", rec)
	}
	if ua := <-uas; ua != m.UserAgent {
		t.Fatalf("server saw User-Agent %q", ua)
	}
}

func TestRunOursRateLimitedMarked(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer ts.Close()
	m := testManifest(ts.URL, testContent(1<<20))
	m.MaxRetries = 0
	rec := (&runner{m: m, slot: "T"}).run(Planned{Server: m.Servers[0], Arm: Arm{Kind: "ours", N: 1, S: 1 << 20}}, 0)
	if rec.Valid || !rec.RateLimited || rec.Err == "" {
		t.Fatalf("429 not recorded as rate-limited invalid run: %+v", rec)
	}
}

func TestRunCurl(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	content := testContent(1<<20 + 7)
	ts := rangeServer(t, content)
	m := testManifest(ts.URL, content)
	rec := (&runner{m: m, slot: "T", curl: "curl"}).run(Planned{Server: m.Servers[0], Arm: Arm{Kind: "curl"}}, 0)
	if !rec.Valid {
		t.Fatalf("curl run invalid: %+v", rec)
	}
}

func TestLoadDoneResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if d, err := loadDone(path); err != nil || len(d) != 0 {
		t.Fatalf("missing file: %v %v", d, err)
	}
	var buf bytes.Buffer
	for _, r := range []Record{{Server: "a", Arm: "curl", Rep: 0}, {Server: "b", Arm: "ours_N4_S1M", Rep: 1, Valid: false}} {
		line, _ := json.Marshal(r)
		buf.Write(append(line, '\n'))
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := loadDone(path)
	if err != nil {
		t.Fatal(err)
	}
	// Invalid runs count as done: they are kept, not silently retried.
	if !d["a|curl|0"] || !d["b|ours_N4_S1M|1"] || len(d) != 2 {
		t.Fatalf("done set %v", d)
	}
}

func TestTimelineBuckets(t *testing.T) {
	start := time.Unix(1000, 0)
	tl := NewTimeline(start, 100*time.Millisecond)
	tl.ObserveBytes(fetcher.ByteEvent{Time: start.Add(10 * time.Millisecond), Delta: 5})
	tl.ObserveBytes(fetcher.ByteEvent{Time: start.Add(90 * time.Millisecond), Delta: 5})
	tl.ObserveBytes(fetcher.ByteEvent{Time: start.Add(350 * time.Millisecond), Delta: 7})
	tl.ObserveBytes(fetcher.ByteEvent{Time: start.Add(-time.Millisecond), Delta: 1})
	if got, want := tl.Buckets(), []int64{11, 0, 0, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buckets %v, want %v", got, want)
	}
}
