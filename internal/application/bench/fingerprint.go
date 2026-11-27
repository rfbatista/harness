package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"operators-mcp/internal/domain"
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

// SetupRecord is what a scorecard keeps of the setup: the fingerprint, the
// models and CLI, the counts and a digest per part (see BenchSetupRecord).
func SetupRecord(s Setup) domain.BenchSetupRecord {
	rec := domain.BenchSetupRecord{
		Fingerprint: Fingerprint(s), AgentName: s.AgentName, Model: s.Model, JudgeModel: s.JudgeModel, CLIVersion: s.CLIVersion,
		Skills: len(s.Skills), Instructions: len(s.Instructions),
		Parts: map[string]string{
			"agent":       digest(s.AgentName + "\x00" + s.AgentPrompt),
			"model":       digest(s.Model),
			"judge model": digest(s.JudgeModel),
			"claude cli":  digest(s.CLIVersion),
		},
	}
	if s.AgentName != "" {
		rec.Agents = 1
	}
	for _, f := range s.Skills {
		rec.Parts["skill "+f.Path] = digest(f.Content)
	}
	for _, f := range s.Instructions {
		rec.Parts["instruction "+f.Path] = digest(f.Content)
	}
	return rec
}

// changedParts lists the parts whose digest differs between two records, or
// nil when either did not record its parts.
func changedParts(prev, cur domain.BenchSetupRecord) []string {
	if prev.Parts == nil || cur.Parts == nil {
		return nil
	}
	out := []string{}
	for name, h := range cur.Parts {
		if prev.Parts[name] != h {
			out = append(out, name)
		}
	}
	for name := range prev.Parts {
		if _, kept := cur.Parts[name]; !kept {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
