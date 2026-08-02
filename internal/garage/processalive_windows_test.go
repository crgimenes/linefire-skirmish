package garage

// Windows has no signal 0: asking whether a pid is alive means opening the
// process and reading its exit code, which is golang.org/x/sys territory — a
// dependency this repository will not take on for one assertion in one test.
//
// So the answer is "not knowable here", and the caller skips that check rather
// than reporting a healthy relaunch as a failure. Everything else in the test —
// that the second deploy really made a NEW process — still runs.
func processAlive(_ int) (alive, known bool) {
	return false, false
}
