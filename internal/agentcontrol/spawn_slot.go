package agentcontrol

import (
	"context"
	"sync"
)

// spawnSlotReservation bridges the process-local preparation window before
// Manager.Spawn makes a worker visible as running. maxParallel is an
// AgentControl-local limit, not a cross-process session semaphore; worker
// execution leases separately prevent two app-servers from running the same
// durable worker ID. Without this reservation, concurrent callers on one
// control can all observe the same free slot and oversubscribe maxParallel.
type spawnSlotReservation struct {
	control *AgentControl
	once    sync.Once
}

func (c *AgentControl) tryReserveSpawnSlot() (*spawnSlotReservation, bool) {
	if c == nil || c.manager == nil {
		return nil, false
	}
	c.spawnSlotMu.Lock()
	defer c.spawnSlotMu.Unlock()
	if c.manager.CountRunning()+c.spawnSlots >= c.maxParallel {
		return nil, false
	}
	c.spawnSlots++
	return &spawnSlotReservation{control: c}, true
}

func (r *spawnSlotReservation) release() {
	r.releaseWithQueueKick(false)
}

func (r *spawnSlotReservation) releaseAndKickQueued() {
	r.releaseWithQueueKick(true)
}

func (r *spawnSlotReservation) releaseWithQueueKick(kickQueued bool) {
	if r == nil || r.control == nil {
		return
	}
	r.once.Do(func() {
		c := r.control
		c.spawnSlotMu.Lock()
		if c.spawnSlots > 0 {
			c.spawnSlots--
		}
		c.spawnSlotMu.Unlock()

		// Direct preparation uses kickQueued because a very short worker can
		// finish before Manager.Spawn returns. Queue drains already own their
		// retry/loop policy and release without a kick, avoiding recursive drains.
		if kickQueued && c.queuedWorkEnabled() && c.hasQueuedSpawns() {
			go c.maybeStartQueued(context.Background())
		}
	})
}

func (c *AgentControl) hasQueuedSpawns() bool {
	if c == nil {
		return false
	}
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	return len(c.queued) > 0
}
