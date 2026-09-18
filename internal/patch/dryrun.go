package patch

// Problem is what is wrong with one block, found before anything was written.
type Problem struct {
	// Index is the block's position in the proposal, 0-based.
	Index int
	// File is the path the block named, as the agent wrote it.
	File string
	Err  error
}

// DryRun reports what applying these blocks would refuse, one entry per bad
// block, and writes nothing.
//
// It exists because the review is per block: the person accepts a subset, so
// the useful unit is "which of these is broken", not "the patch is refused".
// Running it before the review turns the cheapest signal there is - it costs
// no tokens and no execution - into something the person sees while deciding,
// instead of an error after they already decided.
func DryRun(workspace string, blocks []Block, seen Seen) []Problem {
	_, problems := plan(workspace, blocks, seen)
	return problems
}
