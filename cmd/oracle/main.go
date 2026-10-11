// Command oracle performs the formal Stage 0 oracle/headroom analysis using
// only the frozen protocol-v2 fixed-C pilot, the frozen oracle preregistration
// v3 (nonnegative formal tau), and the frozen v3 tau campaign. It runs no
// network experiments and computes no new tau values.
//
// Formal results use the nonnegative formal tau table. The signed-tau
// historical reproduction and the zero-tau variant are diagnostics only.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"adaptivedl/internal/experiment/tau"
)

const workloadBytes = tau.ByteFloor // B_e = 5 MiB

type planEpoch struct {
	EpochIndex         int        `json:"epoch_index"`
	StartMs            int64      `json:"start_ms"`
	EndMs              int64      `json:"end_ms"`
	Opportunities      int        `json:"opportunities"`
	Bytes              int64      `json:"bytes"`
	RateBytesPerSecond float64    `json:"rate_bytes_per_second"`
	Regime             tau.Regime `json:"regime"`
}

type planFile struct {
	TraceID string      `json:"trace_id"`
	Epochs  []planEpoch `json:"epochs"`
}

type resultEpoch struct {
	EpochIndex      int     `json:"epoch_index"`
	RBytesPerSecond float64 `json:"r_bytes_per_second"`
	Valid           bool    `json:"valid"`
}

type resultFile struct {
	TraceID   string `json:"trace_id"`
	C         int    `json:"c"`
	Durations map[string]struct {
		Epochs []resultEpoch `json:"epochs"`
	} `json:"durations"`
}

type aggDirection struct {
	FromC              int                `json:"from_c"`
	ToC                int                `json:"to_c"`
	PooledTauSeconds   float64            `json:"pooled_tau_seconds"`
	RegimeSpecific     bool               `json:"regime_specific"`
	PerRegimeTau       map[string]float64 `json:"per_regime_tau"`
	FormalTauSeconds   float64            `json:"formal_tau_seconds"`
	FormalPerRegimeTau map[string]float64 `json:"formal_per_regime_tau"`
}

type aggFile struct {
	Directions []aggDirection `json:"directions"`
}

type traceData struct {
	TraceID string
	Epochs  []tau.OracleEpoch
}

type traceResult struct {
	TraceID       string
	Fixed         map[int]float64
	BestFixed     float64
	BestC         int
	Instantaneous float64
	Formal        tau.OracleSolution
	H             float64
	Zero          tau.OracleSolution
	ZeroH         float64
	SignedV3      tau.OracleSolution
	SignedV3H     float64
	SignedV2      tau.OracleSolution
	SignedV2H     float64
	ScheduleOK    bool
	StaticOK      bool
	InstOK        bool
}

type cTime struct {
	C     int     `json:"c"`
	Value float64 `json:"value"`
}

func main() {
	pilot := flag.String("pilot", "/home/vscode/epoch_pilot_v2", "protocol-v2 fixed-C pilot directory")
	tauDir := flag.String("tau", "/home/vscode/tau_campaign_v3", "frozen v3 tau campaign directory")
	historicalTau := flag.String("historical-tau", "/home/vscode/tau_campaign_v2", "historical signed-tau campaign (diagnostic only)")
	out := flag.String("out", "/home/vscode/oracle_stage_v3", "analysis output directory")
	epochKey := flag.String("epoch", "2m0s", "selected epoch duration key in result.json")
	prereg := flag.String("prereg", "testdata/oracle_preregistration_v3/oracle_preregistration.json", "frozen preregistration file")
	flag.Parse()

	if err := run(*pilot, *tauDir, *historicalTau, *out, *epochKey, *prereg); err != nil {
		fmt.Fprintln(os.Stderr, "oracle:", err)
		os.Exit(2)
	}
}

func run(pilot, tauDir, historicalTau, out, epochKey, prereg string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var pilotReport struct {
		Selected string `json:"selected_duration"`
		Passed   bool   `json:"passed"`
	}
	if err := readJSON(filepath.Join(pilot, "pilot_report.json"), &pilotReport); err != nil {
		return err
	}
	if !pilotReport.Passed || pilotReport.Selected != epochKey {
		return fmt.Errorf("pilot report mismatch: selected=%q passed=%v", pilotReport.Selected, pilotReport.Passed)
	}
	preregHash, err := sha256File(prereg)
	if err != nil {
		return err
	}
	plans := map[string]planFile{}
	for _, f := range []string{"evdo_plan.json", "lte_plan.json"} {
		var p planFile
		if err := readJSON(filepath.Join(tauDir, "plan", f), &p); err != nil {
			return err
		}
		plans[p.TraceID] = p
	}
	type rateInfo struct {
		rate  float64
		valid bool
	}
	byEpoch := map[string]map[int]map[int]rateInfo{}
	inputHashes := map[string]string{}
	for traceID := range plans {
		byEpoch[traceID] = map[int]map[int]rateInfo{}
		for _, c := range tau.Ladder {
			path := filepath.Join(pilot, fmt.Sprintf("%s_C%d", traceID, c), "result.json")
			h, err := sha256File(path)
			if err != nil {
				return err
			}
			inputHashes[fmt.Sprintf("%s_C%d/result.json", traceID, c)] = h
			var r resultFile
			if err := readJSON(path, &r); err != nil {
				return err
			}
			dur, ok := r.Durations[epochKey]
			if !ok {
				return fmt.Errorf("%s C=%d missing duration %s", traceID, c, epochKey)
			}
			for _, e := range dur.Epochs {
				if byEpoch[traceID][e.EpochIndex] == nil {
					byEpoch[traceID][e.EpochIndex] = map[int]rateInfo{}
				}
				byEpoch[traceID][e.EpochIndex][c] = rateInfo{rate: e.RBytesPerSecond, valid: e.Valid}
			}
		}
	}
	expected := map[string]int{"Verizon-EVDO-driving": 7, "Verizon-LTE-driving": 11}
	var traces []traceData
	for traceID, p := range plans {
		labelByIndex := map[int]tau.Regime{}
		for _, e := range p.Epochs {
			labelByIndex[e.EpochIndex] = e.Regime
		}
		var idxs []int
		for idx, perC := range byEpoch[traceID] {
			if len(perC) != len(tau.Ladder) {
				continue
			}
			valid := true
			for _, c := range tau.Ladder {
				if !perC[c].valid {
					valid = false
					break
				}
			}
			if valid {
				idxs = append(idxs, idx)
			}
		}
		sort.Ints(idxs)
		if len(idxs) != expected[traceID] {
			return fmt.Errorf("%s shared-valid epochs = %d, want %d", traceID, len(idxs), expected[traceID])
		}
		td := traceData{TraceID: traceID}
		for _, idx := range idxs {
			label, ok := labelByIndex[idx]
			if !ok {
				return fmt.Errorf("%s epoch %d has no preregistered regime label", traceID, idx)
			}
			rates := map[int]float64{}
			for _, c := range tau.Ladder {
				rates[c] = byEpoch[traceID][idx][c].rate
			}
			td.Epochs = append(td.Epochs, tau.OracleEpoch{Index: idx, Regime: label, Rates: rates})
		}
		traces = append(traces, td)
	}
	sort.Slice(traces, func(i, j int) bool { return traces[i].TraceID < traces[j].TraceID })

	// Formal v3 table (nonnegative formal values) and diagnostics.
	var formalAgg aggFile
	if err := readJSON(filepath.Join(tauDir, "tau_aggregation.json"), &formalAgg); err != nil {
		return err
	}
	formalTable := buildTable(formalAgg, true)
	signedV3Table := buildTable(formalAgg, false)
	var historicalAgg aggFile
	if err := readJSON(filepath.Join(historicalTau, "tau_aggregation.json"), &historicalAgg); err != nil {
		return err
	}
	signedV2Table := buildTable(historicalAgg, false)

	var results []traceResult
	allSanity := true
	for _, td := range traces {
		fixed, err := tau.FixedCompletion(td.Epochs, workloadBytes)
		if err != nil {
			return err
		}
		best := fixed[tau.Ladder[0]]
		bestC := tau.Ladder[0]
		for _, c := range tau.Ladder {
			if fixed[c] < best {
				best, bestC = fixed[c], c
			}
		}
		inst, err := tau.Instantaneous(td.Epochs, workloadBytes)
		if err != nil {
			return err
		}
		formal, err := tau.SolveConstrained(td.Epochs, workloadBytes, formalTable)
		if err != nil {
			return err
		}
		zero, err := tau.SolveConstrained(td.Epochs, workloadBytes, formalTable.ZeroTau())
		if err != nil {
			return err
		}
		sv3, err := tau.SolveConstrained(td.Epochs, workloadBytes, signedV3Table)
		if err != nil {
			return err
		}
		sv2, err := tau.SolveConstrained(td.Epochs, workloadBytes, signedV2Table)
		if err != nil {
			return err
		}
		tr := traceResult{
			TraceID: td.TraceID, Fixed: fixed, BestFixed: best, BestC: bestC,
			Instantaneous: inst, Formal: formal, H: (best - formal.Objective) / best,
			Zero: zero, ZeroH: (best - zero.Objective) / best,
			SignedV3: sv3, SignedV3H: (best - sv3.Objective) / best,
			SignedV2: sv2, SignedV2H: (best - sv2.Objective) / best,
			ScheduleOK: tau.ValidateSchedule(formal.Sequence) == nil,
			StaticOK:   formal.Objective <= best+1e-9,
			InstOK:     inst <= formal.Objective+1e-9,
		}
		if !tr.ScheduleOK || !tr.StaticOK || !tr.InstOK {
			allSanity = false
		}
		results = append(results, tr)
	}
	if err := writeArtifacts(out, traces, results, formalTable, inputHashes, preregHash, epochKey, tauDir, historicalTau); err != nil {
		return err
	}
	for _, r := range results {
		fmt.Printf("%s: T_best_fixed=%.6f (C%d) T_inst=%.6f T_con=%.6f H=%.6f transitions=%d schedule_ok=%v static_ok=%v inst_ok=%v | signed-v3 H=%.6f signed-v2 H=%.6f zero H=%.6f\n",
			r.TraceID, r.BestFixed, r.BestC, r.Instantaneous, r.Formal.Objective, r.H,
			r.Formal.Transitions, r.ScheduleOK, r.StaticOK, r.InstOK, r.SignedV3H, r.SignedV2H, r.ZeroH)
	}
	if !allSanity {
		return fmt.Errorf("formal sanity inequality violated: see oracle_analysis.txt (no clamping applied)")
	}
	return nil
}

func buildTable(agg aggFile, formal bool) tau.TauTable {
	table := tau.TauTable{
		Pooled:         map[[2]int]float64{},
		RegimeSpecific: map[[2]int]bool{},
		RegimeMedians:  map[[2]int]map[tau.Regime]float64{},
	}
	for _, d := range agg.Directions {
		key := [2]int{d.FromC, d.ToC}
		table.RegimeSpecific[key] = d.RegimeSpecific
		if formal {
			table.Pooled[key] = d.FormalTauSeconds
			if d.RegimeSpecific && len(d.FormalPerRegimeTau) > 0 {
				m := map[tau.Regime]float64{}
				for r, v := range d.FormalPerRegimeTau {
					m[tau.Regime(r)] = v
				}
				table.RegimeMedians[key] = m
			}
			continue
		}
		table.Pooled[key] = d.PooledTauSeconds
		if d.RegimeSpecific && len(d.PerRegimeTau) > 0 {
			m := map[tau.Regime]float64{}
			for r, v := range d.PerRegimeTau {
				m[tau.Regime(r)] = v
			}
			table.RegimeMedians[key] = m
		}
	}
	return table
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

type fixedTraceView struct {
	TraceID           string  `json:"trace_id"`
	RetainedEpochs    int     `json:"retained_epochs"`
	TFixed            []cTime `json:"t_fixed_seconds"`
	TBestFixedSeconds float64 `json:"t_best_fixed_seconds"`
	BestCDescriptive  int     `json:"best_c_descriptive"`
}

type instTraceView struct {
	TraceID               string  `json:"trace_id"`
	TInstantaneousSeconds float64 `json:"t_instantaneous_seconds"`
}

type constrainedTraceView struct {
	TraceID           string                 `json:"trace_id"`
	TConstrainedSecs  float64                `json:"t_constrained_seconds"`
	Transitions       int                    `json:"transitions"`
	Sequence          []int                  `json:"sequence"`
	TransitionDetails []tau.TransitionDetail `json:"transition_details"`
}

type headroomTraceView struct {
	TraceID           string  `json:"trace_id"`
	TBestFixedSeconds float64 `json:"t_best_fixed_seconds"`
	TConstrainedSecs  float64 `json:"t_constrained_seconds"`
	H                 float64 `json:"h"`
}

type tauDirectionView struct {
	FromC            int                `json:"from_c"`
	ToC              int                `json:"to_c"`
	Policy           string             `json:"policy"`
	PooledTauSeconds float64            `json:"pooled_tau_seconds"`
	RegimeMedians    map[string]float64 `json:"regime_medians,omitempty"`
	FormalTauSeconds float64            `json:"formal_tau_seconds"`
}

type tauUsedTransition struct {
	TraceID    string  `json:"trace_id"`
	EpochIndex int     `json:"epoch_index"`
	FromC      int     `json:"from_c"`
	ToC        int     `json:"to_c"`
	Regime     string  `json:"regime"`
	TauSeconds float64 `json:"tau_seconds"`
}

type diagnosticTraceView struct {
	TraceID          string  `json:"trace_id"`
	TConstrainedSecs float64 `json:"t_constrained_seconds"`
	H                float64 `json:"h"`
	Sequence         []int   `json:"sequence"`
}

func writeArtifacts(out string, traces []traceData, results []traceResult, formalTable tau.TauTable, inputHashes map[string]string, preregHash, epochKey, tauDir, historicalTau string) error {
	fixed := struct {
		WorkloadBytes int64            `json:"workload_bytes"`
		WorkloadNote  string           `json:"workload_note"`
		SelectedEpoch string           `json:"selected_epoch"`
		Traces        []fixedTraceView `json:"traces"`
	}{workloadBytes, "B_e = 5 MiB for every C; t[c,e] = B_e/R[c,e]", epochKey, nil}
	inst := struct {
		DiagnosticOnly bool            `json:"diagnostic_only"`
		Note           string          `json:"note"`
		Traces         []instTraceView `json:"traces"`
	}{true, "chooses max_C R[c,e] per epoch; ignores tau, ladder and dwell; not the formal gate", nil}
	constrained := struct {
		Traces []constrainedTraceView `json:"traces"`
	}{}
	headroom := struct {
		Traces            []headroomTraceView `json:"traces"`
		StatisticalStatus map[string]any      `json:"statistical_status"`
	}{StatisticalStatus: map[string]any{
		"n_traces": 2, "unit": "trace", "final_statistical_gate": "PENDING",
		"requirement": ">=5 usable diverse traces with CI_95_lower(H) >= 0.10",
		"note":        "two-trace result is descriptive only; no generalization claim",
	}}
	tauUsed := struct {
		Directions      []tauDirectionView  `json:"directions"`
		TransitionsUsed []tauUsedTransition `json:"transitions_used"`
		Note            string              `json:"note"`
	}{Note: "formal v3 nonnegative tau; raw signed observations are diagnostics only"}
	seqView := struct {
		Traces []constrainedTraceView `json:"traces"`
	}{}
	signed := struct {
		Label      string `json:"label"`
		Note       string `json:"note"`
		Historical struct {
			Source string                `json:"source"`
			Traces []diagnosticTraceView `json:"traces"`
		} `json:"historical_v2_signed"`
		FreshSigned struct {
			Source string                `json:"source"`
			Traces []diagnosticTraceView `json:"traces"`
		} `json:"fresh_v3_signed"`
	}{Label: "HISTORICAL DIAGNOSTIC - NOT FORMAL",
		Note: "reproduction of the §22.72 signed-tau analysis (v2 table) plus the fresh v3 signed estimates; neither is used in the formal gate"}
	signed.Historical.Source = historicalTau
	signed.FreshSigned.Source = tauDir
	zero := struct {
		Label  string                `json:"label"`
		Note   string                `json:"note"`
		Traces []diagnosticTraceView `json:"traces"`
	}{Label: "SECONDARY DIAGNOSTIC - ZERO TRANSITION COST", Note: "all tau set to zero; isolates rate-only headroom"}

	for _, r := range results {
		var fixedTimes []cTime
		for _, c := range tau.Ladder {
			fixedTimes = append(fixedTimes, cTime{C: c, Value: r.Fixed[c]})
		}
		fixed.Traces = append(fixed.Traces, fixedTraceView{
			TraceID: r.TraceID, RetainedEpochs: len(tracesByID(traces, r.TraceID).Epochs),
			TFixed: fixedTimes, TBestFixedSeconds: r.BestFixed, BestCDescriptive: r.BestC,
		})
		inst.Traces = append(inst.Traces, instTraceView{TraceID: r.TraceID, TInstantaneousSeconds: r.Instantaneous})
		con := constrainedTraceView{
			TraceID: r.TraceID, TConstrainedSecs: r.Formal.Objective,
			Transitions: r.Formal.Transitions, Sequence: r.Formal.Sequence,
			TransitionDetails: r.Formal.TransitionAudit,
		}
		constrained.Traces = append(constrained.Traces, con)
		seqView.Traces = append(seqView.Traces, con)
		headroom.Traces = append(headroom.Traces, headroomTraceView{
			TraceID: r.TraceID, TBestFixedSeconds: r.BestFixed,
			TConstrainedSecs: r.Formal.Objective, H: r.H,
		})
		for _, t := range r.Formal.TransitionAudit {
			tauUsed.TransitionsUsed = append(tauUsed.TransitionsUsed, tauUsedTransition{
				TraceID: r.TraceID, EpochIndex: t.EpochIndex, FromC: t.FromC, ToC: t.ToC,
				Regime: string(t.Regime), TauSeconds: t.Tau,
			})
		}
		signed.Historical.Traces = append(signed.Historical.Traces, diagnosticTraceView{
			TraceID: r.TraceID, TConstrainedSecs: r.SignedV2.Objective, H: r.SignedV2H, Sequence: r.SignedV2.Sequence,
		})
		signed.FreshSigned.Traces = append(signed.FreshSigned.Traces, diagnosticTraceView{
			TraceID: r.TraceID, TConstrainedSecs: r.SignedV3.Objective, H: r.SignedV3H, Sequence: r.SignedV3.Sequence,
		})
		zero.Traces = append(zero.Traces, diagnosticTraceView{
			TraceID: r.TraceID, TConstrainedSecs: r.Zero.Objective, H: r.ZeroH, Sequence: r.Zero.Sequence,
		})
	}
	// Formal direction table (from the v3 aggregation JSON, read again for
	// faithful artifact output).
	var formalAgg aggFile
	if err := readJSON(filepath.Join(tauDir, "tau_aggregation.json"), &formalAgg); err != nil {
		return err
	}
	for _, d := range formalAgg.Directions {
		policy := "pooled"
		if d.RegimeSpecific {
			policy = "regime_specific"
		}
		tauUsed.Directions = append(tauUsed.Directions, tauDirectionView{
			FromC: d.FromC, ToC: d.ToC, Policy: policy,
			PooledTauSeconds: d.PooledTauSeconds, RegimeMedians: d.PerRegimeTau,
			FormalTauSeconds: d.FormalTauSeconds,
		})
	}
	if err := writeJSON(filepath.Join(out, "fixed_c_completion.json"), fixed); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "instantaneous_oracle.json"), inst); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "constrained_oracle.json"), constrained); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "headroom.json"), headroom); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "tau_used.json"), tauUsed); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "oracle_sequences.json"), seqView); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "sensitivity_signed_tau.json"), signed); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "sensitivity_zero_tau.json"), zero); err != nil {
		return err
	}
	if err := writeTables(filepath.Join(out, "oracle_tables.txt"), results); err != nil {
		return err
	}
	if err := writeAnalysis(filepath.Join(out, "oracle_analysis.txt"), results); err != nil {
		return err
	}
	manifest := map[string]any{
		"stage":                    "formal oracle/headroom analysis (oracle protocol version 2, nonnegative tau)",
		"fixed_c_protocol_version": 2,
		"oracle_protocol_version":  2,
		"selected_epoch":           epochKey,
		"trace_ids":                []string{"Verizon-EVDO-driving", "Verizon-LTE-driving"},
		"preregistration_sha256":   preregHash,
		"fixed_c_input_sha256":     inputHashes,
		"tau_v3_dir":               tauDir,
		"historical_tau_dir":       historicalTau,
		"formal_tau_source":        "tau_campaign_v3/tau_aggregation.json formal_tau_seconds (nonnegative)",
		"control_source":           "epoch_pilot_v2 protocol-v2 fixed-C pilot (read-only)",
		"headroom_computed":        true,
		"final_statistical_gate":   "PENDING (n=2)",
	}
	return writeJSON(filepath.Join(out, "oracle_manifest.json"), manifest)
}

func tracesByID(traces []traceData, id string) traceData {
	for _, t := range traces {
		if t.TraceID == id {
			return t
		}
	}
	return traceData{}
}

func writeTables(path string, results []traceResult) error {
	var b strings.Builder
	b.WriteString("ORACLE TABLES (formal, oracle protocol v2 / nonnegative tau, B_e = 5 MiB, epoch 120 s)\n")
	b.WriteString("==================================================================================\n\n")
	b.WriteString("TABLE 1 - Fixed-C completion equivalents (seconds)\n")
	b.WriteString("trace                  C=1        C=2        C=4        C=8        C=16       T_best_fixed\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-10.3f %-10.3f %-10.3f %-10.3f %-10.3f %.3f\n", r.TraceID,
			r.Fixed[1], r.Fixed[2], r.Fixed[4], r.Fixed[8], r.Fixed[16], r.BestFixed)
	}
	b.WriteString("\nTABLE 2 - Instantaneous oracle (diagnostic only)\n")
	b.WriteString("trace                  T_instantaneous\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %.3f\n", r.TraceID, r.Instantaneous)
	}
	b.WriteString("\nTABLE 3 - Formal tau (v3, nonnegative)\n")
	b.WriteString("1->2 0.0615 | 2->1 0.0000 | 2->4 0.0021 | 4->2 0.0184 | 4->8 0.0905 | 8->4 0.0000 | 8->16 0.0031 | 16->8 0.0327 (seconds)\n")
	b.WriteString("\nTABLE 4 - Constrained oracle\n")
	b.WriteString("trace                  T_constrained  #transitions  C sequence\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-14.3f %-13d %v\n", r.TraceID, r.Formal.Objective, r.Formal.Transitions, r.Formal.Sequence)
	}
	b.WriteString("\nTABLE 5 - Formal headroom (trace = statistical unit)\n")
	b.WriteString("trace                  T_best_fixed  T_constrained  H\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-13.3f %-14.3f %.4f\n", r.TraceID, r.BestFixed, r.Formal.Objective, r.H)
	}
	b.WriteString("\nTABLE 6 - Historical signed-tau diagnostic (NOT FORMAL)\n")
	b.WriteString("trace                  Formal H   Historical signed-tau H (v2)   Fresh v3 signed H\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-10.4f %-29.4f %.4f\n", r.TraceID, r.H, r.SignedV2H, r.SignedV3H)
	}
	b.WriteString("\nTABLE 7 - Zero-tau diagnostic (NOT FORMAL)\n")
	b.WriteString("trace                  Formal H   Zero-tau H\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-10.4f %.4f\n", r.TraceID, r.H, r.ZeroH)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeAnalysis(path string, results []traceResult) error {
	var b strings.Builder
	b.WriteString("FORMAL ORACLE / HEADROOM ANALYSIS (Stage 0, oracle protocol version 2)\n")
	b.WriteString("=======================================================================\n\n")
	b.WriteString("Inputs: protocol-v2 fixed-C pilot (18 shared-valid 120 s epochs: EVDO 7, LTE 11);\n")
	b.WriteString("frozen preregistration v3 (nonnegative formal tau); fresh v3 tau campaign\n")
	b.WriteString("(72/72 valid observations). Workload: counterfactual completion-time\n")
	b.WriteString("equivalents for B_e = 5 MiB per epoch, the same for every C; these are NOT\n")
	b.WriteString("measured end-to-end download times and the constrained sequence is NOT an\n")
	b.WriteString("actual learned policy or adaptive execution.\n\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%s\n", r.TraceID)
		fmt.Fprintf(&b, "  T_fixed: C1=%.3f C2=%.3f C4=%.3f C8=%.3f C16=%.3f | T_best_fixed=%.3f (C%d)\n",
			r.Fixed[1], r.Fixed[2], r.Fixed[4], r.Fixed[8], r.Fixed[16], r.BestFixed, r.BestC)
		fmt.Fprintf(&b, "  T_instantaneous=%.3f  T_constrained=%.3f  H=%.4f  transitions=%d\n",
			r.Instantaneous, r.Formal.Objective, r.H, r.Formal.Transitions)
		fmt.Fprintf(&b, "  sequence=%v\n", r.Formal.Sequence)
		fmt.Fprintf(&b, "  sanity: schedule_ok=%v static_ok=%v instantaneous_bound_ok=%v\n",
			r.ScheduleOK, r.StaticOK, r.InstOK)
		fmt.Fprintf(&b, "  diagnostics: historical signed-tau H=%.4f | fresh v3 signed H=%.4f | zero-tau H=%.4f\n\n",
			r.SignedV2H, r.SignedV3H, r.ZeroH)
	}
	b.WriteString("Interpretation: the descriptive two-trace headroom is:\n")
	for _, r := range results {
		fmt.Fprintf(&b, "  %s H = %.4f\n", r.TraceID, r.H)
	}
	b.WriteString("The final statistical gate is PENDING: n=2 traces cannot satisfy the\n")
	b.WriteString(">=5 usable diverse traces + CI_95_lower(H) >= 0.10 requirement. No\n")
	b.WriteString("generalization, significance, or Stage 0 PASS is claimed. The signed-tau\n")
	b.WriteString("and zero-tau sections are diagnostics only.\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
