package main

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
)

func TestRenderWindowFollowsTheDirection(t *testing.T) {
	if got := renderWindow(10, 0); !reflect.DeepEqual(got, []int{11, 9, 12, 13, 14}) {
		t.Errorf("a fresh list is read downwards: %v", got)
	}
	if got := renderWindow(10, -1); !reflect.DeepEqual(got, []int{9, 11, 8, 7, 6}) {
		t.Errorf("going up: %v", got)
	}
}

func TestRenderQueueBoundsThePipelines(t *testing.T) {
	q := newRenderQueue[string]()
	keep := []string{"sel", "a", "b", "c"}
	for _, k := range []string{"a", "b", "c"} {
		if q.start(k, keep, false) == nil {
			t.Fatalf("%s must get a slot", k)
		}
	}
	if q.free() || q.start("d", keep, false) != nil {
		t.Errorf("a prefetch waits for a free slot")
	}
	if q.start("a", keep, false) != nil {
		t.Errorf("a render under way is not started twice")
	}
	// the selection takes the slot of the least wanted render, once it reports
	if q.start("sel", keep, true) != nil || !q.inflight["c"].dying || q.inflight["b"].dying {
		t.Fatalf("the selection cancels the least wanted render and waits: %+v", q.inflight["c"])
	}
	if q.start("sel", keep, true) != nil || q.inflight["b"].dying {
		t.Errorf("one dying render is enough: its slot is about to be free")
	}
	q.report("c", "", false, true, true, nil)
	if q.running("c") || q.start("sel", keep, true) == nil {
		t.Errorf("the slot of the cancelled render goes to the selection")
	}
}

func TestRenderQueueCancelsWhatNobodyWants(t *testing.T) {
	q := newRenderQueue[string]()
	ctx := q.start("old", nil, true)
	q.start("near", nil, false)
	q.cancelStale([]string{"sel", "near"})
	if ctx.Err() == nil || !q.inflight["old"].dying || q.inflight["near"].dying {
		t.Fatalf("only the render out of the window is cancelled")
	}
	if !q.running("old") || q.start("old", nil, true) != nil {
		t.Errorf("a dying render keeps its slot until it reports")
	}
}

func TestRenderQueueKeepsPartialsAndFailures(t *testing.T) {
	q := newRenderQueue[string]()
	q.start("k", nil, true)
	q.report("k", "plain", true, false, false, nil)
	if got, ok := q.get("k"); !ok || got != "plain" || q.settled("k") || !q.running("k") {
		t.Fatalf("a partial render is shown and its pipeline goes on")
	}
	q.report("k", "", false, true, false, errors.New("hunk died"))
	if got, _ := q.get("k"); got != "plain" || !q.settled("k") || q.running("k") {
		t.Errorf("what was drawn stays when the renderer dies on the way: %q", got)
	}

	q.start("c", nil, true)
	q.report("c", "plain", true, false, false, nil)
	q.report("c", "", false, true, true, nil)
	if _, ok := q.get("c"); ok || q.settled("c") {
		t.Errorf("a cancelled partial render is dropped, to be rendered again")
	}

	q.start("f", nil, true)
	q.report("f", "", false, true, false, errors.New("bad revision"))
	if q.failed["f"] != "bad revision" || !q.settled("f") {
		t.Errorf("a failure is kept and not tried again: %v", q.failed)
	}
	q.forget("f")
	if q.settled("f") {
		t.Errorf("forget makes it renderable again")
	}
}

func TestRenderQueueIsBounded(t *testing.T) {
	q := newRenderQueue[string]()
	for i := 0; i < maxRenders; i++ {
		if q.report(strconv.Itoa(i), "x", false, true, false, nil) {
			t.Fatalf("dropped at %d", i)
		}
	}
	if q.report("0", "again", false, true, false, nil) {
		t.Errorf("replacing a known render needs no room")
	}
	if !q.report("one more", "x", false, true, false, nil) || len(q.done) != 1 {
		t.Errorf("a full cache is dropped: %d kept", len(q.done))
	}
}
