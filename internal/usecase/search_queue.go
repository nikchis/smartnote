package usecase

import "sync"

// commandKind names an index command.
type commandKind string

// Index commands, as the protocol names them, and cmdRebuilt, which ends a
// rebuild in the queue and goes to no engine.
const (
	cmdReset   commandKind = "reset"
	cmdUpsert  commandKind = "upsert"
	cmdRemove  commandKind = "remove"
	cmdRebuilt commandKind = "rebuilt"
)

// indexCommand is one command for the engine: notes for cmdUpsert, removals
// for cmdRemove, the number of its rebuild for cmdRebuilt, nothing for
// cmdReset.
type indexCommand struct {
	kind     commandKind
	notes    []IndexedNote
	removals []IndexedRemoval
	rebuild  uint64
}

// indexQueue is the unbounded, ordered queue of index commands (spec §3.3
// rule 1). push never blocks; one consumer waits on ready and pops until the
// queue is empty. It is safe for concurrent use.
type indexQueue struct {
	mu   sync.Mutex
	cmds []indexCommand

	// signal holds at most one wake-up: a push after the consumer took it
	// leaves a new one, so no command is left waiting unnoticed.
	signal chan struct{}
}

func newIndexQueue() *indexQueue {
	return &indexQueue{signal: make(chan struct{}, 1)}
}

// push appends cmds in order and wakes the consumer.
func (q *indexQueue) push(cmds ...indexCommand) {
	q.mu.Lock()
	q.cmds = append(q.cmds, cmds...)
	q.mu.Unlock()

	select {
	case q.signal <- struct{}{}:
	default: // a wake-up is already pending
	}
}

// pop removes and returns the first command.
func (q *indexQueue) pop() (indexCommand, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.cmds) == 0 {
		return indexCommand{}, false
	}

	c := q.cmds[0]
	q.cmds[0] = indexCommand{} // drop the note texts from the backing array

	q.cmds = q.cmds[1:]
	if len(q.cmds) == 0 {
		q.cmds = nil
	}

	return c, true
}

// clear drops every queued command.
func (q *indexQueue) clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.cmds = nil
}

// len returns the number of queued commands.
func (q *indexQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.cmds)
}

// ready receives a value after a push; the consumer then pops until empty.
func (q *indexQueue) ready() <-chan struct{} { return q.signal }
