package main

// Fuzzy filtering. Without a query the rows keep their path order; with
// one the list is a search result, best match first (rank.go), and the cursor
// starts on it. Matched positions are byte offsets into the displayed text, as
// match.go reports them.

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
	if !hasTerms(q) {
		for _, f := range files {
			rows = append(rows, fileRow{f: f})
		}
		return rows
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	hits := findFields(q, paths)
	for i := range files {
		if h, ok := hits[i]; ok {
			rows = append(rows, fileRow{f: files[i], score: h.Score, idx: h.Any[0]})
		}
	}
	return rank(rows, func(r fileRow) int { return r.score }, nil) // a search result: best match first
}
