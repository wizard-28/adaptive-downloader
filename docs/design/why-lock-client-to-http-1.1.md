# Standardizing on HTTP/1.1 for RL-Driven Multi-Connection Acceleration

## Context

`adaptive-downloader` uses a Reinforcement Learning (RL) policy as its primary decision engine (with Contextual Bandits like LinUCB retained as a secondary option) to dynamically optimize download throughput. The engine optimizes two primary action parameters in real time:

1. Concurrency Level ($N$): The number of active parallel download workers.
2. Chunk Size ($S$): The byte-range interval assigned per request step.

To train an effective RL agent (e.g., Q-learning, PPO, or DQN) capable of modeling state transitions over time across network environments, we must maintain a deterministic relationship between the selected action tuple $(N, S)$, the environment state transitions, and the resulting reward signal (throughput in MB/s).

## Decision

We explicitly enforce **HTTP/1.1** as the standard transport protocol across all fetchers, completely disabling HTTP/2 (ALPN negotiation) and HTTP/3.

In Go's standard library `net/http`, this is configured by setting `TLSNextProto` to an empty, non-nil map on the `http.Transport` instance:

```go
transport := &http.Transport{
    // ...
    // Force HTTP/1.1 by disabling ALPN HTTP/2 negotiation
    TLSNextProto:      make(map[string]func(string, *tls.Conn) http.RoundTripper),
    ForceAttemptHTTP2: false,
}
```

---

## Technical Rationale

### 1. Direct Control Over Physical TCP Sockets

- **HTTP/1.1 Behavior:** Opening $N$ concurrent HTTP/1.1 worker requests forces the OS to instantiate $N$ distinct TCP connections. Each connection maintains an independent TCP Congestion Window ($cwnd$).
- **Why it matters for RL:** Scaling $N$ directly expands wire capacity, bypassing single-socket throughput limits. Under HTTP/2, worker requests are multiplexed over a **single** underlying TCP socket. If HTTP/2 were enabled, scaling $N$ would split the bandwidth of one socket rather than opening new network capacity, breaking the action-space dynamics for the RL agent.

### 2. Markov Decision Process (MDP) & Reward Stationarity

- **Non-Stationarity Risk:** Automatic protocol negotiation causes some servers to negotiate HTTP/2 while others fall back to HTTP/1.1. Executing action tuple $(N=8, S=5\text{ MB})$ would yield completely different state transitions and reward distributions across targets.
- **Stable MDP State Space:** Standardizing on HTTP/1.1 guarantees that environment transitions and reward functions remain stationary across target servers, enabling both full RL algorithms and secondary contextual bandit policies to generalize without transport-level meta-features.

### 3. Bypassing Per-Connection Edge Rate Limits

Content Delivery Networks (CDNs) and object storage services (e.g., AWS S3) frequently cap throughput **per TCP connection** (e.g., max 2 MB/s per socket).

- Under HTTP/1.1, opening 8 worker connections creates 8 independent sockets, achieving up to 16 MB/s aggregate throughput.
- Under HTTP/2, all 8 multiplexed streams share a single socket, capping total throughput at 2 MB/s regardless of $N$.

### 4. Industry Standard Parity

This design aligns with production multi-connection download accelerators like Internet Download Manager (IDM) and `aria2c`, which achieve high speeds by opening multiple parallel HTTP/1.1 byte-range connections rather than relying on single-socket stream multiplexing.

---

## Trade-offs & Mitigation

| Challenge                          | HTTP/1.1 Implication                                                                                     | Mitigation Strategy                                                                                                                                                  |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **TCP/TLS Handshake Overhead**     | Opening $N$ connections incurs connection establishment delays (1–2 RTTs).                               | **HTTP Keep-Alive:** Maintain persistent connection pools (`MaxIdleConnsPerHost = N`) across worker requests.                                                        |
| **Cold vs. Warm Connection Noise** | Initial chunk on a connection incurs handshake latency and small initial $cwnd$, creating noisy rewards. | **Minimum Chunk Bounds ($S$):** Enforce minimum chunk sizes ($S \ge 1\text{ MB}$) so handshake duration represents a negligible fraction of the chunk download time. |
| **Head-of-Line (HoL) Blocking**    | Packet loss on one connection stalls that specific socket stream.                                        | **Independent Worker Isolation:** `rangetracker` isolates failed or delayed chunks, allowing other workers to continue unimpeded while partial ranges are requeued.  |
