package domain

import "slices"

// The branch block (ADR 0088) keeps an agent session on its session branch:
// while it is on for the session's mode, the gate refuses branch creation,
// switch and local deletion outright. One server-wide setting per mode, on
// while unset.

// SettingBranchBlockModePrefix starts the settings key of each mode's branch
// block; the mode value ends it, "default" for the default mode.
const SettingBranchBlockModePrefix = "branches.block.mode."

// BranchBlockOn and BranchBlockOff are the values a branch block key accepts
// besides "", which clears it back to the built-in.
const (
	BranchBlockOn  = "on"
	BranchBlockOff = "off"
)

// BranchBlockBuiltIn is the value of every mode while its key is unset.
const BranchBlockBuiltIn = BranchBlockOn

// BranchBlockKey is the settings key holding a mode's branch block.
func BranchBlockKey(mode SessionMode) string {
	if mode == SessionModeDefault {
		return SettingBranchBlockModePrefix + "default"
	}
	return SettingBranchBlockModePrefix + string(mode)
}

// ModeName is the name of a session mode as the Server page shows it.
func ModeName(mode SessionMode) string {
	switch mode {
	case SessionModeArchitect:
		return "Architect"
	case SessionModeImplement:
		return "Implement"
	case SessionModeDesign:
		return "Design"
	case SessionModeProduct:
		return "Product"
	case SessionModeDesignSystem:
		return "Design system"
	default:
		return "Default"
	}
}

// BranchBlock is one row of the branch block read.
type BranchBlock struct {
	Mode SessionMode
	Key  string
	Name string
	// Blocked is true when the stored value is on, or the key is unset.
	Blocked bool
}

// BranchBlocks reads every mode's branch block through read, which returns a
// setting's stored value ("" while unset), in the order a person reads the
// modes.
func BranchBlocks(read func(key string) string) []BranchBlock {
	out := make([]BranchBlock, 0, len(modeModelModes))
	for _, mode := range modeModelModes {
		key := BranchBlockKey(mode)
		out = append(out, BranchBlock{Mode: mode, Key: key, Name: ModeName(mode), Blocked: read(key) != BranchBlockOff})
	}
	return out
}

// BranchBlockedFor says whether the branch block is on for a session of mode,
// and the name the refusal gives the mode. A mode that is not one of the six
// is Default.
func BranchBlockedFor(mode SessionMode, read func(key string) string) (blocked bool, name string) {
	if !slices.Contains(modeModelModes, mode) {
		mode = SessionModeDefault
	}
	return read(BranchBlockKey(mode)) != BranchBlockOff, ModeName(mode)
}

// Blocked says whether the branch block covers the action: creating,
// switching and local deletion. Rename, push and remote changes are not.
func (k BranchActionKind) Blocked() bool {
	switch k {
	case BranchCreate, BranchSwitch, BranchDelete:
		return true
	}
	return false
}

// BranchBlockedMessage is what both layers of the gate tell the agent when
// the branch block refuses an action.
func BranchBlockedMessage(modeName, branch string) string {
	return "BRANCH_ACTION_BLOCKED: creating, switching and deleting branches is blocked for " + modeName +
		" sessions. Stay on your session branch " + branch + ". Pushing still goes through a branch request."
}

func validateBranchBlockSetting(key, value string) error {
	if value == "" || value == BranchBlockOn || value == BranchBlockOff {
		return nil
	}
	return &StructuredError{Code: "INVALID_INPUT", Message: key + ` must be "on" or "off"`}
}
