package observability

import (
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// Metrics is a small process-local metrics registry. It intentionally keeps
// only counters and a latency total, which is enough for a single-user daemon
// diagnostics screen without introducing a remote metrics dependency.
type Metrics struct {
	requests        atomic.Uint64
	requestErrors   atomic.Uint64
	requestDuration atomic.Int64
	activeStreams   atomic.Int64
	reconnects      atomic.Uint64
	queueRetries    atomic.Uint64
	queueDead       atomic.Uint64
	policyDenials   atomic.Uint64
}

type Snapshot struct {
	Requests           uint64 `json:"requests"`
	RequestErrors      uint64 `json:"request_errors"`
	RequestDurationMS  int64  `json:"request_duration_ms"`
	ActiveEventStreams int64  `json:"active_event_streams"`
	Reconnects         uint64 `json:"reconnects"`
	QueueRetries       uint64 `json:"queue_retries"`
	QueueDead          uint64 `json:"queue_dead"`
	PolicyDenials      uint64 `json:"policy_denials"`
}

func New() *Metrics { return &Metrics{} }

func (m *Metrics) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	return Snapshot{
		Requests:           m.requests.Load(),
		RequestErrors:      m.requestErrors.Load(),
		RequestDurationMS:  m.requestDuration.Load(),
		ActiveEventStreams: m.activeStreams.Load(),
		Reconnects:         m.reconnects.Load(),
		QueueRetries:       m.queueRetries.Load(),
		QueueDead:          m.queueDead.Load(),
		PolicyDenials:      m.policyDenials.Load(),
	}
}

func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if m == nil {
			c.Next()
			return
		}
		started := time.Now()
		c.Next()
		m.requests.Add(1)
		m.requestDuration.Add(time.Since(started).Milliseconds())
		if c.Writer.Status() >= 400 {
			m.requestErrors.Add(1)
		}
	}
}

func (m *Metrics) StreamOpened() {
	if m != nil {
		m.activeStreams.Add(1)
	}
}

func (m *Metrics) StreamClosed() {
	if m != nil {
		m.activeStreams.Add(-1)
	}
}

func (m *Metrics) Reconnect() {
	if m != nil {
		m.reconnects.Add(1)
	}
}

func (m *Metrics) QueueRetry() {
	if m != nil {
		m.queueRetries.Add(1)
	}
}

func (m *Metrics) QueueDead() {
	if m != nil {
		m.queueDead.Add(1)
	}
}

func (m *Metrics) PolicyDenied() {
	if m != nil {
		m.policyDenials.Add(1)
	}
}
