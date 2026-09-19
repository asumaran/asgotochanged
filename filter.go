package main

// Fuzzy filtering. Rows keep their path order while filtering (the list reads
// like the tree, not like a ranking); the score only decides where the cursor
// lands. Matched positions are byte offsets into the path, as sahilm/fuzzy
// reports them.

import (
	"github.com/sahilm/fuzzy"
)

// fileRow is one changed file in the list.
type fileRow struct {
	f     changedFile
	score int
	idx   []int // matched bytes in the path
}

// filterFiles matches the query against the paths only: a status letter would
// match half the list.
func filterFiles(files []changedFile, q string) []fileRow {
	rows := make([]fileRow, 0, len(files))
	if q == "" {
		for _, f := range files {
			rows = append(rows, fileRow{f: f})
		}
		return rows
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	hits := map[int]fileRow{}
	for _, mt := range fuzzy.Find(q, paths) {
		hits[mt.Index] = fileRow{f: files[mt.Index], score: mt.Score, idx: append([]int(nil), mt.MatchedIndexes...)}
	}
	for i := range files {
		if r, ok := hits[i]; ok {
			rows = append(rows, r)
		}
	}
	return rows
}

// bestIndex returns the position of the highest score; ties go to the first
// row. -1 for an empty list.
func bestIndex(n int, score func(int) int) int {
	best := -1
	for i := 0; i < n; i++ {
		if best == -1 || score(i) > score(best) {
			best = i
		}
	}
	return best
}
