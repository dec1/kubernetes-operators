// Unit tests for the Queue: order, duplicates and delayed adds.
//
// Tested: names come out in the order added; a name already waiting is not
// added twice; a taken name can be added again; Take gives up after its
// timeout; AddAfter adds the name later, not at once.
//
// Not tested here: several goroutines adding and taking at the same time
// (beyond what `go test -race` sees in the other tests), and the exact length
// of AddAfter's delay (only "not at once, but within 2 s").
package core_test

import (
	"testing"
	"time"

	"controlloop/internal/core"
)

func take(t *testing.T, queue *core.Queue, timeout time.Duration) string {
	t.Helper()
	name, _ := queue.Take(timeout)
	return name // "" when nothing arrived.
}

func TestNamesAreTakenInOrder(t *testing.T) {
	queue := core.NewQueue()
	queue.Add("a")
	queue.Add("b")
	if got := take(t, queue, 0); got != "a" {
		t.Errorf("first = %q, want a", got)
	}
	if got := take(t, queue, 0); got != "b" {
		t.Errorf("second = %q, want b", got)
	}
}

func TestAWaitingNameIsNotAddedTwice(t *testing.T) {
	queue := core.NewQueue()
	queue.Add("demo")
	queue.Add("demo")
	if got := take(t, queue, 0); got != "demo" {
		t.Errorf("first = %q, want demo", got)
	}
	if got := take(t, queue, 0); got != "" {
		t.Errorf("second = %q, want nothing", got)
	}
}

func TestATakenNameCanBeAddedAgain(t *testing.T) {
	queue := core.NewQueue()
	queue.Add("demo")
	take(t, queue, 0)
	queue.Add("demo")
	if got := take(t, queue, 0); got != "demo" {
		t.Errorf("got %q, want demo", got)
	}
}

func TestTakeReturnsNothingWhenNothingArrives(t *testing.T) {
	if _, ok := core.NewQueue().Take(10 * time.Millisecond); ok {
		t.Error("want nothing")
	}
}

func TestAddAfterAddsTheNameLater(t *testing.T) {
	queue := core.NewQueue()
	queue.AddAfter("demo", 10*time.Millisecond)
	if got := take(t, queue, 0); got != "" {
		t.Errorf("at once: got %q, want nothing yet", got)
	}
	if got := take(t, queue, 2*time.Second); got != "demo" {
		t.Errorf("later: got %q, want demo", got)
	}
}
