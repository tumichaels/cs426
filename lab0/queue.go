package lab0

import "sync"

// Queue is a simple FIFO queue that is unbounded in size.
// Push may be called any number of times and is not
// expected to fail or overwrite existing entries.
type Queue[T any] struct {
	// Add your fields here
	head        int
	firstEmpty  int
	numElements int
	buf         []T
	size        int
}

// NewQueue returns a new queue which is empty.
func NewQueue[T any]() *Queue[T] {
	return &Queue[T]{
		head:        0,
		firstEmpty:  0,
		numElements: 0,
		buf:         make([]T, 8),
		size:        8,
	}
}

// Push adds an item to the end of the queue.
func (q *Queue[T]) Push(t T) {
	if q.numElements < q.size {
		q.buf[q.firstEmpty] = t
		q.firstEmpty = (q.firstEmpty + 1) % q.size
		q.numElements += 1
		return
	} else {
		// copy elements
		tmp := make([]T, q.size*2)
		n := q.size - q.head
		copy(tmp[:n], q.buf[q.head:q.head+n])
		copy(tmp[n:n+q.firstEmpty], q.buf[:q.firstEmpty])

		// re index buffer
		q.head = 0
		q.firstEmpty = q.size
		q.buf = tmp
		q.size = q.size * 2

		// push new element
		q.buf[q.firstEmpty] = t
		q.firstEmpty += 1
		q.numElements += 1
	}
}

// Pop removes an item from the beginning of the queue
// and returns it unless the queue is empty.
//
// If the queue is empty, returns the zero value for T and false.
//
// If you are unfamiliar with "zero values", consider revisiting
// this section of the Tour of Go: https://go.dev/tour/basics/12
func (q *Queue[T]) Pop() (T, bool) {
	if q.numElements == 0 {
		var dflt T
		return dflt, false
	} else {
		out := q.buf[q.head]
		q.head = (q.head + 1) % q.size
		q.numElements -= 1
		return out, true
	}
}

// ConcurrentQueue provides the same semantics as Queue but
// is safe to access from many goroutines at once.
//
// You can use your implementation of Queue[T] here.
//
// If you are stuck, consider revisiting this section of
// the Tour of Go: https://go.dev/tour/concurrency/9
type ConcurrentQueue[T any] struct {
	mu sync.Mutex
	q  *Queue[T]
}

func NewConcurrentQueue[T any]() *ConcurrentQueue[T] {
	return &ConcurrentQueue[T]{
		q: NewQueue[T](),
	}
}

// Push adds an item to the end of the queue
func (q *ConcurrentQueue[T]) Push(t T) {
	q.mu.Lock()
	q.q.Push(t)
	q.mu.Unlock()
}

// Pop removes an item from the beginning of the queue.
// Returns a zero value and false if empty.
func (q *ConcurrentQueue[T]) Pop() (T, bool) {
	q.mu.Lock()
	val, isEmpty := q.q.Pop()
	q.mu.Unlock()
	return val, isEmpty
}
