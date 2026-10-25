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
	"adaptivedl/internal/policy"
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

// ---- opt-in arm policies ----

func policyManifestJSON(t *testing.T, arms string) string {
	t.Helper()
	return `{"pilot":"p","seed":1,"file":{"name":"f","size":10,"sha256":"` +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		`"},"servers":[{"id":"a","url":"http://x"}],"cells":[{"n":1,"s":1048576}],` +
		`"reps":1,"run_timeout_s":5,"timeline_ms":100` + arms + `}`
}

func TestManifestPolicyArmsParsing(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Old manifest: no new fields, arms unchanged.
	m, err := loadManifest(write("old.json", policyManifestJSON(t, "")))
	if err != nil {
		t.Fatal(err)
	}
	arms := Arms(m)
	if len(arms) != 3 || arms[0].Name() != "ours_N1_S1M" || arms[0].hasPolicyOptions() {
		t.Fatalf("old manifest arms changed: %+v", arms)
	}
	// New manifest.
	m, err = loadManifest(write("new.json", policyManifestJSON(t, `,"policy_arms":[
	 {"kind":"ours","n":16,"s":1048576,"policy":"drop","capped_n":4,"drop_ratio":0.5,"window":3,"no_request_timeout":true,"split":true},
	 {"kind":"ours","n":16,"s":1048576,"policy":"dqn6","dqn_model":"m.json"}]`)))
	if err != nil {
		t.Fatal(err)
	}
	arms = Arms(m)
	if len(arms) != 5 {
		t.Fatalf("arms = %d", len(arms))
	}
	d := arms[1]
	if d.Policy != "drop" || d.CappedN != 4 || d.DropRatio != 0.5 || d.Window != 3 || !d.NoRequestTimeout || !d.Split {
		t.Fatalf("drop arm parsed wrong: %+v", d)
	}
	if arms[1].Name() == arms[2].Name() || arms[1].Name() == arms[0].Name() {
		t.Fatalf("policy arm names collide: %q %q", arms[1].Name(), arms[2].Name())
	}
}

func TestPolicyArmValidation(t *testing.T) {
	base := testManifest("http://x", testContent(10))
	cases := map[string]Arm{
		"unknown policy": {Kind: "ours", N: 4, S: 1 << 20, Policy: "nope"},
		"dqn6 no model":  {Kind: "ours", N: 4, S: 1 << 20, Policy: "dqn6"},
		"model on drop":  {Kind: "ours", N: 4, S: 1 << 20, Policy: "drop", DQNModel: "m"},
		"options no pol": {Kind: "ours", N: 4, S: 1 << 20, CappedN: 2},
		"bad kind":       {Kind: "curl", N: 4, S: 1 << 20, Policy: "drop"},
		"no n":           {Kind: "ours", S: 1 << 20, Policy: "drop"},
		"ratio >= 1":     {Kind: "ours", N: 4, S: 1 << 20, Policy: "drop", DropRatio: 1.5},
		"capped too big": {Kind: "ours", N: 4, S: 1 << 20, Policy: "drop", CappedN: 99},
	}
	for name, a := range cases {
		m := base
		m.PolicyArms = []Arm{a}
		if m.validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	m := base
	m.PolicyArms = []Arm{{Kind: "ours", N: 4, S: 1 << 20, Policy: "drop"}}
	if err := m.validate(); err != nil {
		t.Fatalf("valid drop arm rejected: %v", err)
	}
}

func dropCtx(n int, thr float64) policy.Context {
	return policy.Context{Concurrency: n, Throughput: thr}
}

func TestDropPolicySwitchesOnDrop(t *testing.T) {
	p := newDropPolicy(3, 0.5, 4)
	// Healthy phase: medians 100; no change.
	for i := 0; i < 6; i++ {
		if a := p.SelectAction(dropCtx(16, 100)); a.Kind != policy.ActionNone {
			t.Fatalf("step %d: unexpected action %+v", i, a)
		}
	}
	// Median of last 3 stays >= 50 until two low samples arrive: [100,40,40] -> 40.
	if a := p.SelectAction(dropCtx(16, 40)); a.Kind != policy.ActionNone {
		t.Fatalf("one low sample must not trigger (median 100): %+v", a)
	}
	a := p.SelectAction(dropCtx(16, 40))
	if a.Kind != policy.ActionDecreaseConcurrency || a.Value != 12 {
		t.Fatalf("expected decrease by 12 to reach 4, got %+v", a)
	}
	// One-shot: stays at the capped target even if throughput recovers.
	if a := p.SelectAction(dropCtx(4, 200)); a.Kind != policy.ActionNone {
		t.Fatalf("at target: %+v", a)
	}
	if a := p.SelectAction(dropCtx(8, 200)); a.Kind != policy.ActionDecreaseConcurrency || a.Value != 4 {
		t.Fatalf("still pinned to capped_n: %+v", a)
	}
	if a := p.SelectAction(dropCtx(2, 200)); a.Kind != policy.ActionIncreaseConcurrency || a.Value != 2 {
		t.Fatalf("pinned to capped_n from below: %+v", a)
	}
}

func TestDropPolicyNeverSwitchesWithoutDrop(t *testing.T) {
	p := newDropPolicy(3, 0.5, 4)
	// Noisy but never below half the running max median; includes a ramp-up.
	for i, thr := range []float64{10, 20, 40, 80, 100, 90, 70, 60, 55, 60, 100, 51, 52, 53} {
		if a := p.SelectAction(dropCtx(16, thr)); a.Kind != policy.ActionNone {
			t.Fatalf("sample %d (%v): unexpected action %+v", i, thr, a)
		}
	}
	// Fewer than k samples can never decide.
	q := newDropPolicy(3, 0.5, 4)
	q.SelectAction(dropCtx(16, 100))
	if a := q.SelectAction(dropCtx(16, 1)); a.Kind != policy.ActionNone {
		t.Fatalf("decided before k samples: %+v", a)
	}
	// Zero baseline never triggers.
	z := newDropPolicy(1, 0.5, 4)
	if a := z.SelectAction(dropCtx(16, 0)); a.Kind != policy.ActionNone {
		t.Fatalf("zero baseline triggered: %+v", a)
	}
}

func TestDropArmCompletesAndRecords(t *testing.T) {
	content := testContent(6 << 20)
	ts := rangeServer(t, content)
	m := testManifest(ts.URL, content)
	r := &runner{m: m, slot: "T"}
	arm := Arm{Kind: "ours", N: 8, S: 1 << 20, Policy: "drop", NoRequestTimeout: true, Split: true}
	rec := r.run(Planned{Server: m.Servers[0], Arm: arm, Rep: 0}, 0)
	if !rec.Valid {
		t.Fatalf("run invalid: %+v", rec)
	}
	if rec.Policy != "drop" || rec.PolicyWindow != 3 || rec.FinalN == 0 || len(rec.Decisions) == 0 || !rec.NoReqTimeout || !rec.SplitStragglr {
		t.Fatalf("policy fields missing: %+v", rec)
	}
	// Plain arms keep the old record shape.
	plain := r.run(Planned{Server: m.Servers[0], Arm: Arm{Kind: "ours", N: 2, S: 1 << 20}, Rep: 0}, 1)
	if !plain.Valid || plain.Policy != "" || plain.Decisions != nil || plain.FinalN != 0 {
		t.Fatalf("plain arm record changed: %+v", plain)
	}
}

func TestDQN6ArmCompletes(t *testing.T) {
	model := filepath.Join("..", "..", "testdata", "rl_cap_train_v2", "model", "dqn_cap_v2_candidate.json")
	if _, err := os.Stat(model); err != nil {
		t.Skip("model file absent")
	}
	content := testContent(8 << 20)
	ts := rangeServer(t, content)
	m := testManifest(ts.URL, content)
	r := &runner{m: m, slot: "T"}
	arm := Arm{Kind: "ours", N: 8, S: 1 << 20, Policy: "dqn6", DQNModel: model, NoRequestTimeout: true}
	if err := arm.validatePolicy(); err != nil {
		t.Fatal(err)
	}
	rec := r.run(Planned{Server: m.Servers[0], Arm: arm, Rep: 0}, 0)
	if !rec.Valid || !rec.ChecksumOK {
		t.Fatalf("dqn6 run invalid: %+v", rec)
	}
	raw, _ := os.ReadFile(model)
	sum := sha256.Sum256(raw)
	if rec.Policy != "dqn6" || rec.PolicyWindow != 16 || rec.DQNModel != model ||
		rec.DQNModelSHA != hex.EncodeToString(sum[:]) || len(rec.Decisions) == 0 || rec.FinalN < 1 || rec.FinalN > 16 {
		t.Fatalf("dqn6 record fields wrong: %+v", rec)
	}
	// A missing model file is a recorded run error, not a panic.
	bad := arm
	bad.DQNModel = filepath.Join(t.TempDir(), "absent.json")
	rec = r.run(Planned{Server: m.Servers[0], Arm: bad, Rep: 0}, 1)
	if rec.Valid || rec.Err == "" {
		t.Fatalf("missing model should fail the run: %+v", rec)
	}
}

func TestReferenceArmsFalse(t *testing.T) {
	base := `{"pilot":"p","seed":1,"file":{"name":"f","size":10,"sha256":"` +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		`"},"servers":[{"id":"a","url":"http://x"}],"reps":1,"run_timeout_s":5,"timeline_ms":100`
	arm := `{"kind":"ours","n":16,"s":1048576,"no_request_timeout":true,"split":true}`
	var m Manifest
	// No cells and no policy arms is still rejected, with or without the switch.
	for _, body := range []string{base + `}`, base + `,"reference_arms":false}`} {
		m = Manifest{}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatal(err)
		}
		if m.validate() == nil {
			t.Fatalf("want error for %s", body)
		}
	}
	// Policy arms only, references kept: still needs cells.
	m = Manifest{}
	if err := json.Unmarshal([]byte(base+`,"policy_arms":[`+arm+`]}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.validate() == nil {
		t.Fatal("want error: no cells with reference arms on")
	}
	// Policy arms only, references off: valid, and only the policy arms run.
	m = Manifest{}
	if err := json.Unmarshal([]byte(base+`,"reference_arms":false,"policy_arms":[`+arm+`]}`), &m); err != nil {
		t.Fatal(err)
	}
	if err := m.validate(); err != nil {
		t.Fatal(err)
	}
	if arms := Arms(m); len(arms) != 1 || arms[0].Kind != "ours" {
		t.Fatalf("arms = %+v", arms)
	}
}

// The frozen real-server check (testdata/live_mirror_check) must plan exactly
// 3 servers x 4 arms x 5 blocks, with no aria2 or curl arms.
func TestMirrorCheckManifest(t *testing.T) {
	p := filepath.Join("..", "..", "testdata", "live_mirror_check", "manifest.json")
	if _, err := os.Stat(p); err != nil {
		t.Skip("manifest absent")
	}
	m, err := loadManifest(p)
	if err != nil {
		t.Fatal(err)
	}
	plan := Schedule(m, "M1", m.Servers)
	if len(plan) != 60 {
		t.Fatalf("plan has %d runs, want 60", len(plan))
	}
	count := map[string]int{}
	for _, pr := range plan {
		if pr.Arm.Kind != "ours" {
			t.Fatalf("unexpected arm kind %q", pr.Arm.Kind)
		}
		count[pr.Arm.Name()]++
	}
	if len(count) != 4 {
		t.Fatalf("arms = %v", count)
	}
	for name, c := range count {
		if c != 15 {
			t.Fatalf("arm %s planned %d times, want 15", name, c)
		}
	}
	t.Log(count)
}
