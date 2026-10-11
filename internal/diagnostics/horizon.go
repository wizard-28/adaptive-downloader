// Package diagnostics implements clearly-labeled, one-off diagnostic
// experiments. It is NOT part of the production downloader/experiment path and
// changes no production behavior. Its only purpose is measurement.
//
// The LinUCB learning-horizon study (this file) determines how much
// post-warm-up decision budget a benchmark scenario provides for a given file
// size, without changing any RL/experiment parameter other than the file size.
package diagnostics

import (
	"fmt"
	"sort"

	"adaptivedl/internal/experiment"
	"adaptivedl/internal/experiment/network"
)

// WarmupSelections is the fixed bounded LinUCB warm-up length:
// InitialExplorationRounds (1) * NumActions (5).
const WarmupSelections = 5

// HorizonConfig configures the learning-horizon study.
type HorizonConfig struct {
	// FileSizes are the candidate horizons (bytes). All other scenario
	// parameters are taken unchanged from DefaultMatrixConfig.
	FileSizes []int64
	// Scenarios selects which DefaultMatrixConfig scenarios to run (names). Empty
	// means all of them.
	Scenarios []string
	// Modes defaults to []{ModeLinUCB}.
	Modes []experiment.Mode
	// Repetitions is the number of runs per (file size, scenario, mode).
	Repetitions int
	// NetworkController applies tc/netem for the network scenarios. Nil disables
	// it (those runs are then reported unavailable by the framework).
	NetworkController *network.Controller
}

// RunObservation is one run's horizon measurements.
type RunObservation struct {
	FileSize int64
	Scenario string
	Mode     string
	Rep      int

	Valid bool
	Err   string

	// Total is the number of attempts (== decisions for a policy mode).
	Total int
	// Warmup/Post split the policy decisions. For modes without a policy
	// (fixed) they are -1 (not applicable).
	Warmup int
	Post   int

	WarmupOrder []string

	PostActions map[string]int
	UniquePost  int

	ConcChanges  int
	ChunkChanges int

	FinalConc  int
	FinalChunk int64
	MaxConc    int
	MinChunk   int64
	MaxChunk   int64

	DurationMs   float64
	WallclockBps float64
	Retries      int64
	Failures     int64
}

// HorizonAggregate aggregates observations for one (file size, scenario, mode).
type HorizonAggregate struct {
	FileSize int64
	Scenario string
	Mode     string

	Runs      int
	ValidRuns int

	MeanTotal, MinTotal, MaxTotal float64
	MeanWarmup, MeanPost          float64
	MinPost, MaxPost              int

	MeanUniquePost float64
	PostCounts     map[string]int

	MeanConcChanges  float64
	MeanChunkChanges float64

	MeanFinalConc  float64
	MeanFinalChunk float64
	MeanMaxConc    float64
	MeanMinChunk   float64
	MeanMaxChunk   float64

	MeanDurationMs   float64
	MeanWallclockBps float64
	MeanRetries      float64
	MeanFailures     float64
}

// HorizonReport is the full study output.
type HorizonReport struct {
	FileSizes    []int64
	Repetitions  int
	Scenarios    []string
	Modes        []string
	Observations []RunObservation
	Aggregates   []HorizonAggregate
}

// RunHorizonStudy runs the candidate file sizes across the selected scenarios
// and modes, holding every other parameter at its DefaultMatrixConfig value.
func RunHorizonStudy(cfg HorizonConfig) (HorizonReport, error) {
	if cfg.Repetitions <= 0 {
		cfg.Repetitions = 3
	}
	base := experiment.DefaultMatrixConfig()
	byName := make(map[string]experiment.MatrixScenario, len(base.Scenarios))
	for _, sc := range base.Scenarios {
		byName[sc.Name] = sc
	}
	names := cfg.Scenarios
	if len(names) == 0 {
		for _, sc := range base.Scenarios {
			names = append(names, sc.Name)
		}
	}
	for _, n := range names {
		if _, ok := byName[n]; !ok {
			return HorizonReport{}, fmt.Errorf("unknown scenario %q", n)
		}
	}
	modes := cfg.Modes
	if len(modes) == 0 {
		modes = []experiment.Mode{experiment.ModeLinUCB}
	}

	rep := HorizonReport{FileSizes: append([]int64(nil), cfg.FileSizes...), Repetitions: cfg.Repetitions, Scenarios: names}
	for _, m := range modes {
		rep.Modes = append(rep.Modes, string(m))
	}

	for _, fs := range cfg.FileSizes {
		mscen := make([]experiment.MatrixScenario, 0, len(names))
		for _, n := range names {
			sc := byName[n]
			sc.Config.FileSize = fs
			mscen = append(mscen, sc)
		}
		mr, err := experiment.RunMatrix(experiment.MatrixConfig{
			Scenarios:         mscen,
			Repetitions:       cfg.Repetitions,
			NetworkController: cfg.NetworkController,
			Modes:             modes,
		})
		if err != nil {
			return HorizonReport{}, fmt.Errorf("file size %d: %w", fs, err)
		}
		for _, sr := range mr.Scenarios {
			for _, m := range modes {
				for i, r := range sr.Runs(m) {
					rep.Observations = append(rep.Observations, observeHorizon(fs, sr.Name, m, i, r))
				}
			}
		}
	}
	rep.Aggregates = aggregateHorizon(rep.Observations)
	return rep, nil
}

func observeHorizon(fs int64, scenario string, mode experiment.Mode, rep int, r experiment.RunResult) RunObservation {
	o := RunObservation{
		FileSize:    fs,
		Scenario:    scenario,
		Mode:        string(mode),
		Rep:         rep,
		Valid:       r.Err == "" && r.OutputMatches && r.NoDuplicateRanges && r.NoMissingRanges,
		Err:         r.Err,
		Total:       int(r.TotalAttempts),
		Warmup:      -1,
		Post:        -1,
		PostActions: map[string]int{},
		FinalConc:   r.FinalConcurrency,
		FinalChunk:  r.FinalChunkSize,
		MaxConc:     r.MaxConcurrency,
		MinChunk:    r.FileSize, // placeholder; tightened below for policy modes
		Retries:     r.RetryAttempts,
		Failures:    r.FailedAttempts,
	}
	if r.Duration > 0 {
		o.DurationMs = float64(r.Duration.Milliseconds())
		o.WallclockBps = float64(r.FileSize) / r.Duration.Seconds()
	}
	if len(r.Decisions) == 0 {
		// No policy decisions (e.g. fixed mode). Report chunk bounds from the
		// run result only.
		o.MinChunk = r.MinChunkSize
		o.MaxChunk = r.MaxChunkSize
		return o
	}
	decs := r.Decisions
	o.Total = len(decs)
	if len(decs) >= WarmupSelections {
		o.Warmup = WarmupSelections
	} else {
		o.Warmup = len(decs)
	}
	o.Post = len(decs) - o.Warmup
	minChunk := r.FileSize
	var maxChunk int64
	for i, x := range decs {
		if i < o.Warmup {
			o.WarmupOrder = append(o.WarmupOrder, x.ActionName)
			continue
		}
		o.PostActions[x.ActionName]++
		if x.ConcurrencyAfter != x.Concurrency {
			o.ConcChanges++
		}
		if x.ChunkSizeAfter != x.ChunkSize {
			o.ChunkChanges++
		}
		if x.Concurrency > o.MaxConc {
			o.MaxConc = x.Concurrency
		}
		if x.ConcurrencyAfter > o.MaxConc {
			o.MaxConc = x.ConcurrencyAfter
		}
		for _, c := range []int64{x.ChunkSize, x.ChunkSizeAfter} {
			if c > 0 && c < minChunk {
				minChunk = c
			}
			if c > maxChunk {
				maxChunk = c
			}
		}
	}
	o.UniquePost = len(o.PostActions)
	if minChunk == r.FileSize {
		minChunk = r.FinalChunkSize
	}
	if maxChunk == 0 {
		maxChunk = r.FinalChunkSize
	}
	o.MinChunk, o.MaxChunk = minChunk, maxChunk
	return o
}

func aggregateHorizon(obs []RunObservation) []HorizonAggregate {
	type key struct {
		fs       int64
		scenario string
		mode     string
	}
	groups := map[key][]RunObservation{}
	var order []key
	for _, o := range obs {
		k := key{o.FileSize, o.Scenario, o.Mode}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], o)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].fs != order[j].fs {
			return order[i].fs < order[j].fs
		}
		if order[i].scenario != order[j].scenario {
			return order[i].scenario < order[j].scenario
		}
		return order[i].mode < order[j].mode
	})

	var out []HorizonAggregate
	for _, k := range order {
		g := groups[k]
		a := HorizonAggregate{FileSize: k.fs, Scenario: k.scenario, Mode: k.mode, PostCounts: map[string]int{}}
		a.Runs = len(g)
		var sumDur, sumWC, sumConcCh, sumChunkCh, sumFinalC, sumFinalChunk, sumMaxC, sumMinChunk, sumMaxChunk, sumUnique, sumRetr, sumFail float64
		a.MinTotal = 1 << 30
		a.MinPost = 1 << 30
		var sumTotal, sumPost float64
		postValid := 0
		for _, o := range g {
			if o.Valid {
				a.ValidRuns++
			}
			a.MeanWarmup += float64(o.Warmup)
			total := float64(o.Total)
			sumTotal += total
			if total < a.MinTotal {
				a.MinTotal = total
			}
			if total > a.MaxTotal {
				a.MaxTotal = total
			}
			if o.Post >= 0 {
				postValid++
				sumPost += float64(o.Post)
				if o.Post < a.MinPost {
					a.MinPost = o.Post
				}
				if o.Post > a.MaxPost {
					a.MaxPost = o.Post
				}
				sumUnique += float64(o.UniquePost)
				sumConcCh += float64(o.ConcChanges)
				sumChunkCh += float64(o.ChunkChanges)
				for name, c := range o.PostActions {
					a.PostCounts[name] += c
				}
			}
			sumFinalC += float64(o.FinalConc)
			sumFinalChunk += float64(o.FinalChunk)
			sumMaxC += float64(o.MaxConc)
			sumMinChunk += float64(o.MinChunk)
			sumMaxChunk += float64(o.MaxChunk)
			sumDur += o.DurationMs
			sumWC += o.WallclockBps
			sumRetr += float64(o.Retries)
			sumFail += float64(o.Failures)
		}
		n := float64(len(g))
		a.MeanTotal = sumTotal / n
		a.MeanWarmup /= n
		if postValid > 0 {
			a.MeanPost = sumPost / float64(postValid)
			a.MeanUniquePost = sumUnique / float64(postValid)
			a.MeanConcChanges = sumConcCh / float64(postValid)
			a.MeanChunkChanges = sumChunkCh / float64(postValid)
		} else {
			a.MinPost, a.MaxPost = -1, -1
		}
		a.MeanFinalConc = sumFinalC / n
		a.MeanFinalChunk = sumFinalChunk / n
		a.MeanMaxConc = sumMaxC / n
		a.MeanMinChunk = sumMinChunk / n
		a.MeanMaxChunk = sumMaxChunk / n
		a.MeanDurationMs = sumDur / n
		a.MeanWallclockBps = sumWC / n
		a.MeanRetries = sumRetr / n
		a.MeanFailures = sumFail / n
		out = append(out, a)
	}
	return out
}
