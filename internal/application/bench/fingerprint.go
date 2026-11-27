package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// SetupFile is one file of a setup: a skill or an instruction file, by path.
type SetupFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Setup is the agent setup a bench run ran on, as far as it can change a
// score: the agent, its skills, the instruction files, the session model, the
// model that judges, and the CLI. Its Fingerprint lets a scorecard say that
// two runs ran on the same one.
type Setup struct {
	AgentName    string      `json:"agent_name"`
	AgentPrompt  string      `json:"agent_prompt"`
	Skills       []SetupFile `json:"skills"`
	Instructions []SetupFile `json:"instructions"`
	Model        string      `json:"model"`
	JudgeModel   string      `json:"judge_model"`
	CLIVersion   string      `json:"cli_version"`
}

// Fingerprint is a stable digest of the setup: equal for an unchanged setup,
// whatever order its files were listed in, and different when any part changes.
func Fingerprint(s Setup) string {
	s.Skills = sortedFiles(s.Skills)
	s.Instructions = sortedFiles(s.Instructions)
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sortedFiles(in []SetupFile) []SetupFile {
	out := append([]SetupFile{}, in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
