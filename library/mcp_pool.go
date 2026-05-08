package library

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// mcpPoolKey identifies a poolable MCP server instance. Vertices that share
// the same canonicalized spec (and init_timeout) share warm slots.
type mcpPoolKey struct {
	kind          string
	command       string
	argsCSV       string // "\x00"-joined; order preserved (positional args matter)
	envCSV        string // "\x00"-joined and sorted for canonicalization
	url           string
	headersCSV    string // "\x00"-joined; spec.headers is already sorted
	initTimeoutMs int64
}

func makeMCPPoolKey(spec mcpTransportSpec, initTimeout time.Duration) mcpPoolKey {
	return mcpPoolKey{
		kind:          spec.kind,
		command:       spec.command,
		argsCSV:       strings.Join(spec.args, "\x00"),
		envCSV:        strings.Join(spec.env, "\x00"),
		url:           spec.url,
		headersCSV:    strings.Join(spec.headers, "\x00"),
		initTimeoutMs: initTimeout.Milliseconds(),
	}
}

// mcpPoolEntry holds the warm-session state for one mcpPoolKey.
type mcpPoolEntry struct {
	spec        mcpTransportSpec
	initTimeout time.Duration

	mu       sync.Mutex
	targetN  int
	ready    []*mcpSession // LIFO stack of warm sessions
	inflight int           // replenishment goroutines in progress
}

// mcpPool is the process-global warm-replenish pool. Sessions are produced
// asynchronously by replenishment goroutines and consumed at most once: callers
// own the session after pop and never return it to the pool.
type mcpPool struct {
	ctx    context.Context // canceled by Shutdown to abort in-flight starts
	cancel context.CancelFunc

	mu      sync.Mutex
	entries map[mcpPoolKey]*mcpPoolEntry
	closed  bool
	wg      sync.WaitGroup // tracks replenishment goroutines
}

func newMCPPool() *mcpPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &mcpPool{
		ctx:     ctx,
		cancel:  cancel,
		entries: make(map[mcpPoolKey]*mcpPoolEntry),
	}
}

var globalMCPPool = newMCPPool()

// getOrCreateEntry returns the entry for key, creating it on first use.
// Raises targetN to the maximum of current and requested — vertices that share
// a key but request different pool_sizes converge to the largest.
func (p *mcpPool) getOrCreateEntry(key mcpPoolKey, spec mcpTransportSpec, initTimeout time.Duration, targetN int) *mcpPoolEntry {
	p.mu.Lock()
	e, ok := p.entries[key]
	if !ok {
		e = &mcpPoolEntry{
			spec:        spec,
			initTimeout: initTimeout,
		}
		p.entries[key] = e
	}
	p.mu.Unlock()

	e.mu.Lock()
	if targetN > e.targetN {
		e.targetN = targetN
	}
	e.mu.Unlock()
	return e
}

// topUp schedules replenishment goroutines so that ready+inflight reaches
// targetN. Returns the number of goroutines spawned (zero when already at
// target or when the pool is shut down). Caller must NOT hold e.mu.
func (e *mcpPoolEntry) topUp(p *mcpPool) int {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return 0
	}
	e.mu.Lock()
	deficit := e.targetN - len(e.ready) - e.inflight
	if deficit <= 0 {
		e.mu.Unlock()
		return 0
	}
	e.inflight += deficit
	e.mu.Unlock()
	for i := 0; i < deficit; i++ {
		p.wg.Add(1)
		go e.replenishWorker(p)
	}
	return deficit
}

// replenishWorker starts one MCP session and pushes it onto e.ready. On start
// failure it retries with bounded exponential backoff until pool shutdown.
// Each invocation produces at most one session before exiting.
func (e *mcpPoolEntry) replenishWorker(p *mcpPool) {
	defer p.wg.Done()
	delay := 500 * time.Millisecond
	for {
		if p.ctx.Err() != nil {
			e.mu.Lock()
			e.inflight--
			e.mu.Unlock()
			return
		}
		sess, err := startMCPSessionFromSpec(p.ctx, e.spec, e.initTimeout)
		if err == nil {
			p.mu.Lock()
			closed := p.closed
			p.mu.Unlock()
			e.mu.Lock()
			if closed {
				e.inflight--
				e.mu.Unlock()
				_ = sess.close()
				return
			}
			e.ready = append(e.ready, sess)
			e.inflight--
			ready, inflight := len(e.ready), e.inflight
			e.mu.Unlock()
			slog.Debug("mcp pool replenish ok", "transport", e.spec.kind, "target", e.spec.label(), "ready", ready, "inflight", inflight)
			return
		}
		slog.Warn("mcp pool replenish failed", "transport", e.spec.kind, "target", e.spec.label(), "err", err)
		select {
		case <-p.ctx.Done():
			e.mu.Lock()
			e.inflight--
			e.mu.Unlock()
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

// pop removes and returns the most recently added warm session, if any.
// Also returns the post-pop ready count (a stable snapshot taken under e.mu)
// for logging.
func (e *mcpPoolEntry) pop() (*mcpSession, bool, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.ready) == 0 {
		return nil, false, 0
	}
	n := len(e.ready) - 1
	sess := e.ready[n]
	e.ready[n] = nil
	e.ready = e.ready[:n]
	return sess, true, n
}

// mcpPoolAcquire borrows a warm session if one is available, else starts a
// fresh one synchronously using ctx. The caller is the sole owner: they must
// call sess.close() when done; sessions are never returned to the pool. Each
// successful acquire (warm or sync) schedules a replenishment so the
// steady-state warm count is preserved.
//
// When poolSize <= 0 this is a passthrough to startMCPSessionFromSpec (no pool
// involvement, zero behavior change for callers that don't opt in).
func mcpPoolAcquire(ctx context.Context, spec mcpTransportSpec, initTimeout time.Duration, poolSize int) (*mcpSession, error) {
	if poolSize <= 0 {
		return startMCPSessionFromSpec(ctx, spec, initTimeout)
	}
	p := globalMCPPool
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return startMCPSessionFromSpec(ctx, spec, initTimeout)
	}
	key := makeMCPPoolKey(spec, initTimeout)
	e := p.getOrCreateEntry(key, spec, initTimeout, poolSize)
	sess, ok, remaining := e.pop()
	e.topUp(p)
	if ok {
		slog.DebugContext(ctx, "mcp pool acquire warm hit", "transport", spec.kind, "target", spec.label(), "ready_after", remaining)
		return sess, nil
	}
	slog.DebugContext(ctx, "mcp pool acquire cold miss (sync start)", "transport", spec.kind, "target", spec.label())
	return startMCPSessionFromSpec(ctx, spec, initTimeout)
}

// mcpPoolPrewarm schedules replenishments to fill the pool for the given key
// up to poolSize warm slots. Idempotent: repeated calls converge to the max
// requested poolSize. No-op when poolSize <= 0 or the pool is shut down.
func mcpPoolPrewarm(spec mcpTransportSpec, initTimeout time.Duration, poolSize int) {
	if poolSize <= 0 {
		return
	}
	p := globalMCPPool
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	key := makeMCPPoolKey(spec, initTimeout)
	e := p.getOrCreateEntry(key, spec, initTimeout, poolSize)
	if scheduled := e.topUp(p); scheduled > 0 {
		slog.Debug("mcp pool prewarm scheduled", "transport", spec.kind, "target", spec.label(), "want", poolSize, "starts", scheduled)
	}
}

// ShutdownMCPPool drains the global MCP session pool: cancels in-flight
// replenishments, closes idle warm sessions, and waits up to ctx deadline for
// replenishment goroutines to exit. Sessions currently borrowed by ops are
// untouched — their lifecycle is owned by the caller.
//
// Programs that opt into pooling (vertex param pool_size > 0) should defer
// this from main():
//
//	defer library.ShutdownMCPPool(context.Background())
//
// Safe to call multiple times; subsequent calls are no-ops. After shutdown,
// new mcpPoolAcquire calls fall through to direct startMCPSessionFromSpec
// (graceful degradation, no panic).
func ShutdownMCPPool(ctx context.Context) error {
	p := globalMCPPool
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	entries := make([]*mcpPoolEntry, 0, len(p.entries))
	for _, e := range p.entries {
		entries = append(entries, e)
	}
	p.mu.Unlock()

	p.cancel()

	totalIdle := 0
	for _, e := range entries {
		e.mu.Lock()
		ready := e.ready
		e.ready = nil
		totalIdle += len(ready)
		e.mu.Unlock()
		for _, s := range ready {
			// Subprocess termination on stdin-close commonly returns a
			// non-zero exit code from MCP servers that lack a graceful
			// shutdown RPC. The process is still gone; this is expected
			// noise at shutdown, not a failure.
			if cerr := s.close(); cerr != nil {
				slog.Debug("mcp pool: idle session close exit", "err", cerr)
			}
		}
	}
	slog.Info("mcp pool shutdown begin", "entries", len(entries), "idle_total", totalIdle)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("mcp pool shutdown done")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("ShutdownMCPPool: timed out waiting for replenishment goroutines: %w", ctx.Err())
	}
}
