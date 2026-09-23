package services

import "sync"

// archiveTaskGate tracks work that may retain a *pvf.Archive after it has
// released core.mu. Closing the archive blocks new tasks, cancels the active
// ones, and waits for their references to disappear before releasing memory.
type archiveTaskGate struct {
	mu      sync.Mutex
	cond    *sync.Cond
	closing bool
	nextID  uint64
	tasks   map[uint64]chan struct{}
}

func newArchiveTaskGate() archiveTaskGate {
	gate := archiveTaskGate{tasks: make(map[uint64]chan struct{})}
	gate.cond = sync.NewCond(&gate.mu)
	return gate
}

func (g *archiveTaskGate) ensureLocked() {
	if g.cond != nil {
		return
	}
	g.tasks = make(map[uint64]chan struct{})
	g.cond = sync.NewCond(&g.mu)
}

func (g *archiveTaskGate) begin() func() {
	g.mu.Lock()
	g.ensureLocked()
	for g.closing {
		g.cond.Wait()
	}
	g.nextID++
	id := g.nextID
	done := make(chan struct{})
	g.tasks[id] = done
	g.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			if current, ok := g.tasks[id]; ok {
				delete(g.tasks, id)
				close(current)
			}
			g.mu.Unlock()
		})
	}
}

func (g *archiveTaskGate) beginClose() []<-chan struct{} {
	g.mu.Lock()
	g.ensureLocked()
	for g.closing {
		g.cond.Wait()
	}
	g.closing = true
	waitFor := make([]<-chan struct{}, 0, len(g.tasks))
	for _, done := range g.tasks {
		waitFor = append(waitFor, done)
	}
	g.mu.Unlock()
	return waitFor
}

func (g *archiveTaskGate) endClose() {
	g.mu.Lock()
	g.ensureLocked()
	g.closing = false
	g.cond.Broadcast()
	g.mu.Unlock()
}
