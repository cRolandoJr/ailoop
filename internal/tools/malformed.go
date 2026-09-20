package tools

import "strings"

// MalformedBlock describes a tool block the model meant to send and Parse
// could not read, or "" when there is nothing wrong.
//
// Parse splits on the opener and ignores what it cannot read, which is right:
// guessing at a half-written block would run something nobody asked for. But
// ignoring it in silence is not. Measured against qwen2.5-coder, twice in a
// row: it wrote "**TOOL**" in markdown bold, the request evaporated, the loop
// read the reply as a final answer and ended the phase. No refusal, no
// warning, and the agent believed it had asked for a file.
//
// The two shapes below cannot happen by accident. A closer with no opener, or
// an opener that is never closed, is always someone trying to use a tool.
func MalformedBlock(text string) string {
	hasOpen := strings.Contains(text, openTool)
	hasClose := strings.Contains(text, closeTool)

	switch {
	case hasClose && !hasOpen:
		return "You wrote " + closeTool + " with no " + openTool + " before it, so the request was not read. " +
			"The opener is exactly " + openTool + " on its own line - not bold, not in a code fence."
	case hasOpen && !hasClose:
		return "You opened " + openTool + " and never closed it with " + closeTool + ", so the request was not read."
	}
	return ""
}
