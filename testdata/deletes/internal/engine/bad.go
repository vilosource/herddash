package engine

// Deleting rows here is not allowed; this comment is not code.
func purge(m map[string]int) []string {
	q := "DELETE FROM transitions"
	delete(m, "k")
	return []string{q, `delete from resync_markers`, "rows deleted"}
}
