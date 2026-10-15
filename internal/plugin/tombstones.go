package plugin

import (
	"container/list"
	"time"
)

// Retired request IDs only protect the finite interval in which a canceled
// remote computation can reasonably finish. The hard cap also bounds memory
// when a plugin never replies or cancellation volume is unusually high.
const (
	retiredRequestTTL        = 5 * time.Minute
	maxRetiredRequestEntries = 4096
)

type retiredRequestTombstone struct {
	id        string
	expiresAt time.Time
}

// retiredRequestTombstones is not independently synchronized. pluginProcess
// owns it under pluginProcess.mu so pending and retired transitions are atomic.
type retiredRequestTombstones struct {
	byID  map[string]*list.Element
	order list.List
}

func (t *retiredRequestTombstones) retire(id string, now time.Time) {
	t.cleanup(now)
	if t.byID == nil {
		t.byID = make(map[string]*list.Element)
	}
	if existing := t.byID[id]; existing != nil {
		t.order.Remove(existing)
	}
	element := t.order.PushBack(retiredRequestTombstone{id: id, expiresAt: now.Add(retiredRequestTTL)})
	t.byID[id] = element
	for len(t.byID) > maxRetiredRequestEntries {
		t.removeElement(t.order.Front())
	}
}

func (t *retiredRequestTombstones) consume(id string, now time.Time) bool {
	t.cleanup(now)
	element := t.byID[id]
	if element == nil {
		return false
	}
	t.removeElement(element)
	return true
}

func (t *retiredRequestTombstones) cleanup(now time.Time) {
	for element := t.order.Front(); element != nil; element = t.order.Front() {
		tombstone := element.Value.(retiredRequestTombstone)
		if tombstone.expiresAt.After(now) {
			return
		}
		t.removeElement(element)
	}
}

func (t *retiredRequestTombstones) removeElement(element *list.Element) {
	if element == nil {
		return
	}
	tombstone := element.Value.(retiredRequestTombstone)
	delete(t.byID, tombstone.id)
	t.order.Remove(element)
}

func (t *retiredRequestTombstones) clear() {
	clear(t.byID)
	t.order.Init()
}

func (t *retiredRequestTombstones) len() int {
	return len(t.byID)
}

func (t *retiredRequestTombstones) contains(id string, now time.Time) bool {
	t.cleanup(now)
	return t.byID[id] != nil
}
