package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/patch"
	"github.com/pterm/pterm"
)

// AskApprovalInteractive parses the blocks and allows interactive approval.
// It returns a boolean (if approved at all, or if blocks were approved),
// the reason for rejection (if any), and the list of approved blocks.
func AskApprovalInteractive(proposal string) (bool, string, []patch.Block) {
	blocks, err := patch.ParseBlocks(proposal)
	if err != nil || len(blocks) == 0 {
		// Fallback to normal approval if no blocks (e.g. discovery phase)
		ok, reason := AskApprovalWithReason()
		return ok, reason, nil
	}

	pterm.DefaultHeader.WithFullWidth().Println("PROPOSAL CONTAINS PATCHES")
	fmt.Printf("The agent proposed %d file modifications.\n", len(blocks))

	result, _ := pterm.DefaultInteractiveSelect.
		WithOptions([]string{"Accept All", "Reject All", "Interactive (file by file)"}).
		Show()

	if result == "Accept All" {
		return true, "", blocks
	}

	if result == "Reject All" {
		pterm.Warning.Println("Proposal rejected.")
		pterm.Info.Println("Please provide a reason so the AI can fix the divergence:")
		reader := bufio.NewReader(os.Stdin)
		reason, _ := reader.ReadString('\n')
		return false, strings.TrimSpace(reason), nil
	}

	var approvedBlocks []patch.Block
	var rejectedReasons []string

	for i, b := range blocks {
		searchLines := strings.Split(b.Search, "\n")
		for j, l := range searchLines {
			searchLines[j] = pterm.FgRed.Sprintf("- %s", l)
		}
		replaceLines := strings.Split(b.Replace, "\n")
		for j, l := range replaceLines {
			replaceLines[j] = pterm.FgGreen.Sprintf("+ %s", l)
		}

		diffBox := pterm.DefaultBox.WithTitle(fmt.Sprintf("Patch %d/%d: %s", i+1, len(blocks), b.FilePath)).
			Sprint(fmt.Sprintf("%s\n%s", strings.Join(searchLines, "\n"), strings.Join(replaceLines, "\n")))
		fmt.Println(diffBox)

		res, _ := pterm.DefaultInteractiveConfirm.Show("Approve this patch?")
		if res {
			approvedBlocks = append(approvedBlocks, b)
		} else {
			pterm.Info.Println("Reason for rejecting this specific patch:")
			reader := bufio.NewReader(os.Stdin)
			reason, _ := reader.ReadString('\n')
			rejectedReasons = append(rejectedReasons, fmt.Sprintf("Rejected patch for %s: %s", b.FilePath, strings.TrimSpace(reason)))
		}
	}

	if len(approvedBlocks) == 0 {
		return false, strings.Join(rejectedReasons, "\n"), nil
	}

	// Partial approval: Some blocks were accepted.
	// The problem is that the agent's proposal is partially accepted, meaning the agent's context
	// should probably be updated. But our state machine saves the *entire* proposal if approved.
	// For now, if partial approval, we apply the approved blocks. If there were rejections,
	// we treat it as a rejection to loop back, OR we return true but append the rejections to a new task?
	// The prompt said "Los cambios aprobados se aplicarán y los rechazados se alimentarán como feedback al agente."
	// That means we return false (to loop back), but we return the approved blocks to apply them FIRST.
	return len(rejectedReasons) == 0, strings.Join(rejectedReasons, "\n"), approvedBlocks
}
