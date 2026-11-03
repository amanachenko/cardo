package launcher

// warnLine is a check's state with its action attached. The warnings list once showed the state
// alone, which is how the gold grant's "run cardo gold" stayed invisible while the board shouted
// about the grant.
func warnLine(r Result) string {
	line := r.Name + ": " + r.Value
	if r.Fix != "" {
		line += " — " + r.Fix
	}
	return line
}
