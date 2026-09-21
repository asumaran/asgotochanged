package main

// The renders of a list of diffs: the ones that are done, the ones that
// failed and the ones under way. A diff takes a moment to draw (git, then
// delta or hunk), so the selection is rendered off the update loop and the
// rows around it ahead of time, a few at once. The tool says which renders it
// wants, the most wanted first (the selection, then renderWindow), and starts
// the command; everything else is decided here, the same way in every tool:
//
//   - at most maxPipelines renders run at once, the dying ones included, so
//     holding an arrow key never piles up processes;
//   - a render nobody wants anymore is cancelled. It keeps its slot until it
//     reports back: the process is still dying, and the same key started
//     again now would be killed by the late report of this one;
//   - the selection never waits behind a prefetch: with every slot taken it
//     cancels the least wanted render and starts when that reports back (the
//     tool runs its updatePreview on every report);
//   - a partial render (hunk's first frame: the diff without its syntax
//     highlighting) is kept and shown until the final one replaces it, and it
//     stays if the renderer dies on the way; a cancelled one is dropped;
//   - a failure is kept as such, shown as an error and not tried again;
//   - the finished renders are bounded: when full they are simply dropped
//     (the selection renders again in milliseconds, from the disk cache).
//
// This file is the same in every tool of the family that renders diffs.

import (
	"context"
	"slices"
)

const (
	// maxPipelines bounds the renders running at once.
	maxPipelines = 3
	// prefetchAhead is how many rows are rendered ahead in the direction of
	// travel; the row behind the cursor is rendered too.
	prefetchAhead = 4
	// maxRenders bounds the renders kept in memory.
	maxRenders = 128
)

// pipeline is a render under way. A cancelled one is dying until it reports.
type pipeline struct {
	cancel context.CancelFunc
	dying  bool
}

// renderQueue is the renders by key. R is what a tool keeps of one.
type renderQueue[R any] struct {
	done     map[string]R
	partial  map[string]bool
	failed   map[string]string
	inflight map[string]*pipeline
}

func newRenderQueue[R any]() *renderQueue[R] {
	return &renderQueue[R]{
		done:     map[string]R{},
		partial:  map[string]bool{},
		failed:   map[string]string{},
		inflight: map[string]*pipeline{},
	}
}

// renderWindow is the rows worth having rendered besides the one under the
// cursor, the most useful first: the next one in the direction of travel
// (dir is 1, -1, or 0 for a fresh list, which is read downwards), the one
// behind, then further ahead. The caller skips the ones that do not exist.
func renderWindow(cursor, dir int) []int {
	if dir == 0 {
		dir = 1
	}
	rows := []int{cursor + dir, cursor - dir}
	for i := 2; i <= prefetchAhead; i++ {
		rows = append(rows, cursor+i*dir)
	}
	return rows
}

// get is the render of k to show, a partial one included.
func (q *renderQueue[R]) get(k string) (R, bool) {
	r, ok := q.done[k]
	return r, ok
}

// settled reports whether k needs no render: it is done or it failed.
func (q *renderQueue[R]) settled(k string) bool {
	if _, failed := q.failed[k]; failed {
		return true
	}
	_, done := q.done[k]
	return done && !q.partial[k]
}

// running reports whether a render of k is under way, dying or not.
func (q *renderQueue[R]) running(k string) bool { return q.inflight[k] != nil }

// cancelStale cancels the renders that are not in keep.
func (q *renderQueue[R]) cancelStale(keep []string) {
	for k, p := range q.inflight {
		if !p.dying && !slices.Contains(keep, k) {
			p.cancel()
			p.dying = true
		}
	}
}

// start gives k a slot and returns the context to render it in, or nil when
// it is under way already (a dying one starts again when it reports back) or
// has to wait. keep is what is wanted, the most wanted first; wanted says k
// is the selection, which takes the slot of the least wanted render once that
// has reported back. A prefetch just waits for a free slot.
func (q *renderQueue[R]) start(k string, keep []string, wanted bool) context.Context {
	if q.inflight[k] != nil {
		return nil
	}
	if len(q.inflight) >= maxPipelines {
		dying := false
		for _, p := range q.inflight {
			dying = dying || p.dying
		}
		if wanted && !dying { // a dying render frees its slot in a moment
			for i := len(keep) - 1; i >= 0; i-- {
				if p := q.inflight[keep[i]]; p != nil && keep[i] != k {
					p.cancel()
					p.dying = true
					break
				}
			}
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	q.inflight[k] = &pipeline{cancel: cancel}
	return ctx
}

// free reports whether a prefetch could start now.
func (q *renderQueue[R]) free() bool { return len(q.inflight) < maxPipelines }

// report files what the pipeline of k reported: r, a partial frame or the
// final one, or how it ended. last says the pipeline is over. It returns true
// when the finished renders were dropped to make room, for the tool to drop
// what it keeps next to them.
func (q *renderQueue[R]) report(k string, r R, partial, last, cancelled bool, err error) (dropped bool) {
	if p := q.inflight[k]; p != nil && last {
		p.cancel()
		delete(q.inflight, k)
	}
	switch {
	case cancelled:
		// A partial render is not worth keeping: it would never get refined.
		if q.partial[k] {
			delete(q.done, k)
			delete(q.partial, k)
		}
	case err != nil && q.partial[k]:
		delete(q.partial, k) // the renderer died on the way: what it drew is as good as it gets
	case err != nil:
		q.failed[k] = err.Error()
	default:
		if _, known := q.done[k]; !known && len(q.done) >= maxRenders {
			q.done, q.partial, q.failed = map[string]R{}, map[string]bool{}, map[string]string{}
			dropped = true
		}
		q.done[k] = r
		if partial {
			q.partial[k] = true
		} else {
			delete(q.partial, k)
		}
	}
	return dropped
}

// forget drops what is kept of k, for it to be rendered again.
func (q *renderQueue[R]) forget(k string) {
	delete(q.done, k)
	delete(q.partial, k)
	delete(q.failed, k)
}
