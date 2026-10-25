package domain

import (
	"regexp"
	"sort"
)

// Limits of a layer preview.
const (
	PreviewSampleSize  = 20
	PreviewMaxOverlaps = 50
)

// PreviewError says why a layer's match could not be applied.
type PreviewError struct {
	Code    string `json:"code"` // INVALID_REGEX | INVALID_FOLDER
	Message string `json:"message"`
}

// LayerPreview is what one layer of a draft catches: the files whose
// narrowest layer it is.
type LayerPreview struct {
	Name      string        `json:"name"`
	FileCount int           `json:"file_count"`
	Sample    []string      `json:"sample"`
	Error     *PreviewError `json:"error"`
}

// UnlayeredPreview is the files no layer catches.
type UnlayeredPreview struct {
	FileCount int      `json:"file_count"`
	Sample    []string `json:"sample"`
}

// LayerOverlap is a file that two or more layers match, with the layers in
// the order of their precedence: the winner first.
type LayerOverlap struct {
	Path   string   `json:"path"`
	Layers []string `json:"layers"`
	Winner string   `json:"winner"`
}

// LayersPreview is what a draft of the layers catches in a file list.
type LayersPreview struct {
	Layers    []LayerPreview   `json:"layers"`
	Unlayered UnlayeredPreview `json:"unlayered"`
	Overlaps  []LayerOverlap   `json:"overlaps"`
}

// PreviewLayers applies layers to files (sorted, as the caller got them):
// every file belongs to the one layer that wins for it, or to none. A layer
// whose match is invalid catches nothing and carries its error.
func PreviewLayers(layers []ArchLayer, files []string) LayersPreview {
	out := LayersPreview{Layers: make([]LayerPreview, len(layers)), Unlayered: UnlayeredPreview{Sample: []string{}}, Overlaps: []LayerOverlap{}}
	var valid []ArchLayer
	var index []int // valid[i] is layers[index[i]]
	for i, l := range layers {
		out.Layers[i] = LayerPreview{Name: l.Name, Sample: []string{}}
		if e := matchError(l.Match); e != nil {
			out.Layers[i].Error = e
			continue
		}
		valid = append(valid, l)
		index = append(index, i)
	}
	rec := ArchitectureRecord{Layers: valid}
	compiled := rec.Compile()
	sort.Strings(files)
	for _, f := range files {
		matching := compiled.Matching(f)
		winner := compiled.LayerOf(f)
		if winner < 0 {
			out.Unlayered.FileCount++
			if len(out.Unlayered.Sample) < PreviewSampleSize {
				out.Unlayered.Sample = append(out.Unlayered.Sample, f)
			}
			continue
		}
		lp := &out.Layers[index[winner]]
		lp.FileCount++
		if len(lp.Sample) < PreviewSampleSize {
			lp.Sample = append(lp.Sample, f)
		}
		if len(matching) > 1 && len(out.Overlaps) < PreviewMaxOverlaps {
			names := []string{valid[winner].Name}
			for _, m := range matching {
				if m != winner {
					names = append(names, valid[m].Name)
				}
			}
			out.Overlaps = append(out.Overlaps, LayerOverlap{Path: f, Layers: names, Winner: valid[winner].Name})
		}
	}
	return out
}

func matchError(m ArchLayerMatch) *PreviewError {
	switch {
	case m.Regex != "":
		if _, err := regexp.Compile(m.Regex); err != nil {
			return &PreviewError{Code: ArchInvalidRegex, Message: err.Error()}
		}
	case m.Folder != "":
		if msg := badFolder(m.Folder); msg != "" {
			return &PreviewError{Code: ArchInvalidFolder, Message: msg}
		}
	default:
		return &PreviewError{Code: ArchInvalidFolder, Message: "a layer matches a folder or a regex"}
	}
	return nil
}

// ArchitectureCandidate is an instruction file, tool or sandbox found in the
// checkout or the harness, for the person to confirm.
type ArchitectureCandidate struct {
	// Path is an instruction's repository-relative path.
	Path string `json:"path,omitempty"`
	// Name is a tool's or sandbox's name.
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Loaded string `json:"loaded,omitempty"`
	// Source says where it was found: a file, or a part of the harness.
	Source   string   `json:"source,omitempty"`
	CanReach []string `json:"can_reach,omitempty"`
}

// ArchitectureDiscovery is what a repository and the harness offer for the
// descriptive parts of an architecture, and the agent names the access matrix
// has rows for.
type ArchitectureDiscovery struct {
	Instructions []ArchitectureCandidate `json:"instructions"`
	Tools        []ArchitectureCandidate `json:"tools"`
	Sandboxes    []ArchitectureCandidate `json:"sandboxes"`
	Agents       []string                `json:"agents"`
}
