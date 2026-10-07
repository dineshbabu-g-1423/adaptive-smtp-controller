package scheduler

import (
	"container/heap"
	"sync"
	"time"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

// pqItem wraps a message for the priority queue.
type pqItem struct {
	msg   model.Message
	index int
}

// A priorityQueue orders by NextAttempt (earliest first), then Priority (high first).
type priorityQueue []*pqItem

func (pq priorityQueue) Len() int { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool {
	a, b := pq[i].msg, pq[j].msg
	if !a.NextAttempt.Equal(b.NextAttempt) {
		return a.NextAttempt.Before(b.NextAttempt)
	}
	return a.Priority > b.Priority
}
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *priorityQueue) Push(x any) {
	it := x.(*pqItem)
	it.index = len(*pq)
	*pq = append(*pq, it)
}
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	return it
}

// Queue is a thread-safe, time-ordered priority queue of messages.
type Queue struct {
	mu sync.Mutex
	pq priorityQueue
}

// NewQueue returns an empty queue.
func NewQueue() *Queue {
	q := &Queue{}
	heap.Init(&q.pq)
	return q
}

// Enqueue adds a message, defaulting NextAttempt/EnqueuedAt if unset.
func (q *Queue) Enqueue(m model.Message) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if m.EnqueuedAt.IsZero() {
		m.EnqueuedAt = time.Now()
	}
	if m.NextAttempt.IsZero() {
		m.NextAttempt = time.Now()
	}
	heap.Push(&q.pq, &pqItem{msg: m})
}

// PopReady returns the earliest message whose NextAttempt <= now, or false.
func (q *Queue) PopReady(now time.Time) (model.Message, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pq.Len() == 0 {
		return model.Message{}, false
	}
	top := q.pq[0]
	if top.msg.NextAttempt.After(now) {
		return model.Message{}, false
	}
	it := heap.Pop(&q.pq).(*pqItem)
	return it.msg, true
}

// Depth returns the number of queued messages.
func (q *Queue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pq.Len()
}
