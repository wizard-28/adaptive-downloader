package dqn

import "time"

// Transition is one sequential RL transition: (s_t, a_t, r_t, s_{t+1}, done).
//
// Credit is genuine sequential credit: the action a_t was selected in state s_t,
// the environment advanced, s_{t+1} was observed, and r_t is the negative
// elapsed wall-clock time between those two observations.
type Transition struct {
	State     []float64 `json:"state"`
	Action    int       `json:"action"`
	Reward    float64   `json:"reward"`
	NextState []float64 `json:"next_state"`
	Done      bool      `json:"done"`
	Elapsed   float64   `json:"elapsed_s"`
}

// StepResult is the environment's response to Step(action).
type StepResult struct {
	State     Observation   `json:"-"`
	Elapsed   time.Duration `json:"elapsed"`
	Reward    float64       `json:"reward"` // = -Elapsed seconds
	Done      bool          `json:"done"`
	Truncated bool          `json:"truncated"`
	// ActionEffectiveness reports what the action actually did.
	Requested      int   `json:"requested_action"`
	Effective      int   `json:"effective_action"` // may differ if clamped
	NoOp           bool  `json:"no_op"`
	Concurrency    int   `json:"concurrency"`
	ChunkSize      int64 `json:"chunk_size"`
	RemainingBytes int64 `json:"remaining_bytes"`
}

// Environment is the episodic RL environment abstraction. The DQN trainer works
// against this interface and is never coupled to downloader internals.
type Environment interface {
	// Reset begins a new episode and returns the initial state features.
	Reset() ([]float64, error)
	// Step applies an action index, advances the environment, and returns the
	// transition outcome. reward is negative elapsed wall-clock seconds.
	Step(action int) (StepResult, []float64, error)
	// State returns the current raw observation.
	State() Observation
	// Done reports whether the episode has terminated (completion or truncation).
	Done() bool
	// MaxEpisodeSteps is the explicit truncation bound (0 means unbounded).
	MaxEpisodeSteps() int
}
