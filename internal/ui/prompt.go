package ui

import (
	"bufio"
	"os"
	"strings"

	"github.com/pterm/pterm"
)

// AskApprovalWithReason asks the user for approval. If rejected, asks for a reason.
func AskApprovalWithReason() (bool, string) {
	result, _ := pterm.DefaultInteractiveConfirm.Show("Do you approve this proposal?")
	if result {
		return true, ""
	}

	pterm.Warning.Println("Proposal rejected.")
	pterm.Info.Println("Please provide a reason so the AI can fix the divergence:")

	reader := bufio.NewReader(os.Stdin)
	reason, _ := reader.ReadString('\n')
	return false, strings.TrimSpace(reason)
}
