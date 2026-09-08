package policy

import "time"

type Action struct {
	Concurrency int
	ChunkSize   int64
}

func (a Action) Valid() bool {
	return a.Concurrency > 0 && a.ChunkSize > 0
}

// Context This is the initial download context
// TODO: Expand this later
type Context struct {
	Throughput        float64
	PerConnectionRate float64
	Latency           time.Duration
	ErrorRate         float64
	RetryRate         float64

	ActiveConnections  int
	CurrentConcurrency int
	CurrentChunkSize   int64
}

type Policy interface {
	SelectAction() Action
	Observe(ctx Context)
}
