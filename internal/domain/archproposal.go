package domain

import (
	"encoding/json"
	"fmt"
)

// An architecture proposal (ADR 0060) is a list of item-level changes to a
// repository's architecture record. The assistant makes them; the server
// applies them to the person's draft to validate the result, and the web UI
// applies the same changes when the person accepts. Nothing here stores
// anything: only the person's save does.

// Proposal operations.
const (
	OpAdd    = "add"
	OpChange = "change"
	OpRemove = "remove"
)

// Proposal target kinds.
const (
	TargetPattern         = "pattern"
	TargetLayer           = "layer"
	TargetLayerAccess     = "layer_access"
	TargetUnlayeredAccess = "unlayered_access"
	TargetRule            = "rule"
	TargetInstruction     = "instruction"
	TargetTool            = "tool"
	TargetSandbox         = "sandbox"
	TargetRouting         = "routing"
	TargetHandoff         = "handoff"
)

// DefaultAccessAgent is the agent name of a layer's default cell.
const DefaultAccessAgent = "*"

// ProposalTarget names the item a change is about. Which fields it uses
// depends on Kind: a layer has a Name, a layer's access cell a Layer and an
// Agent, a rule an ID, an instruction a Path, a hand-off a From and a To.
type ProposalTarget struct {
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"`
	Layer string `json:"layer,omitempty"`
	Agent string `json:"agent,omitempty"`
	ID    string `json:"id,omitempty"`
	Path  string `json:"path,omitempty"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

// ProposalChange is one change of a proposal. Value is the item's new value in
// the record's JSON shape (absent on a remove). Base is the target's value in
// the draft the proposal was made against, null when it did not exist there.
type ProposalChange struct {
	Target ProposalTarget  `json:"target"`
	Op     string          `json:"op"`
	Value  json.RawMessage `json:"value,omitempty"`
	Base   json.RawMessage `json:"base"`
	Reason string          `json:"reason"`
}

// ApplyProposal applies changes, in order, to a copy of rec and returns it.
// add and change set the item, remove deletes it, and a new item is appended
// to its list. A change that cannot be applied (an unknown kind or op, a
// missing key, a value of the wrong shape, an access cell of a layer that does
// not exist) is the error; rec is never changed.
func ApplyProposal(rec ArchitectureRecord, changes []ProposalChange) (ArchitectureRecord, error) {
	out, err := cloneRecord(rec)
	if err != nil {
		return rec, err
	}
	for i, c := range changes {
		if err := applyChange(&out, c); err != nil {
			return rec, fmt.Errorf("change %d (%s %s): %w", i+1, c.Op, c.Target.Kind, err)
		}
	}
	return out, nil
}

// ProposalBase is the value of target in rec in the record's JSON shape, or
// null when it does not exist. For a layer being changed it leaves out the
// access, which a change does not touch; op says which.
func ProposalBase(rec ArchitectureRecord, t ProposalTarget, op string) (json.RawMessage, error) {
	var v any
	switch t.Kind {
	case TargetPattern:
		if rec.Pattern != "" {
			v = rec.Pattern
		}
	case TargetLayer:
		if l := findIn(rec.Layers, func(l ArchLayer) bool { return l.Name == t.Name }); l >= 0 {
			layer := rec.Layers[l]
			if op == OpChange {
				return layerWithoutAccess(layer)
			}
			v = layer
		}
	case TargetLayerAccess:
		l := findIn(rec.Layers, func(l ArchLayer) bool { return l.Name == t.Layer })
		if l < 0 {
			break
		}
		v = accessCell(rec.Layers[l].Access, t.Agent)
	case TargetUnlayeredAccess:
		v = accessCell(rec.Unlayered, t.Agent)
	case TargetRule:
		if i := findIn(rec.Rules, func(r ArchRule) bool { return r.ID == t.ID }); i >= 0 {
			v = rec.Rules[i]
		}
	case TargetInstruction:
		if i := findIn(rec.Instructions, func(x ArchInstruction) bool { return x.Path == t.Path }); i >= 0 {
			v = rec.Instructions[i]
		}
	case TargetTool:
		if i := findIn(rec.Tools, func(x ArchTool) bool { return x.Name == t.Name }); i >= 0 {
			v = rec.Tools[i]
		}
	case TargetSandbox:
		if i := findIn(rec.Sandboxes, func(x ArchSandbox) bool { return x.Name == t.Name }); i >= 0 {
			v = rec.Sandboxes[i]
		}
	case TargetRouting:
		if i := findIn(rec.Orchestration.Routing, func(x ArchRoute) bool { return x.Agent == t.Agent }); i >= 0 {
			v = rec.Orchestration.Routing[i]
		}
	case TargetHandoff:
		if i := findIn(rec.Orchestration.Handoffs, func(x ArchHandoff) bool { return x.From == t.From && x.To == t.To }); i >= 0 {
			v = rec.Orchestration.Handoffs[i]
		}
	default:
		return nil, fmt.Errorf("unknown target kind %q", t.Kind)
	}
	return json.Marshal(v)
}

func layerWithoutAccess(l ArchLayer) (json.RawMessage, error) {
	b, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	delete(m, "access")
	return json.Marshal(m)
}

// accessCell is an access cell's level, nil when the agent has no entry.
func accessCell(a LayerAccess, agent string) any {
	if agent == DefaultAccessAgent {
		return a.Default
	}
	if l, ok := a.Agents[agent]; ok {
		return l
	}
	return nil
}

func cloneRecord(rec ArchitectureRecord) (ArchitectureRecord, error) {
	var out ArchitectureRecord
	b, err := json.Marshal(rec)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

func applyChange(r *ArchitectureRecord, c ProposalChange) error {
	if c.Op != OpAdd && c.Op != OpChange && c.Op != OpRemove {
		return fmt.Errorf("unknown op %q", c.Op)
	}
	t, remove := c.Target, c.Op == OpRemove
	switch t.Kind {
	case TargetPattern:
		if remove {
			r.Pattern = ""
			return nil
		}
		return decodeValue(c.Value, &r.Pattern)
	case TargetLayer:
		if err := need(t.Name, "name"); err != nil {
			return err
		}
		return applyLayer(r, t.Name, c)
	case TargetLayerAccess:
		if err := need(t.Layer, "layer"); err != nil {
			return err
		}
		i := findIn(r.Layers, func(l ArchLayer) bool { return l.Name == t.Layer })
		if i < 0 {
			return fmt.Errorf("no layer %q", t.Layer)
		}
		return applyAccess(&r.Layers[i].Access, t.Agent, c)
	case TargetUnlayeredAccess:
		return applyAccess(&r.Unlayered, t.Agent, c)
	case TargetRule:
		if err := need(t.ID, "id"); err != nil {
			return err
		}
		return applyItem(&r.Rules, func(x ArchRule) bool { return x.ID == t.ID }, func(x *ArchRule) { x.ID = t.ID }, c)
	case TargetInstruction:
		if err := need(t.Path, "path"); err != nil {
			return err
		}
		return applyItem(&r.Instructions, func(x ArchInstruction) bool { return x.Path == t.Path }, func(x *ArchInstruction) { x.Path = t.Path }, c)
	case TargetTool:
		if err := need(t.Name, "name"); err != nil {
			return err
		}
		return applyItem(&r.Tools, func(x ArchTool) bool { return x.Name == t.Name }, func(x *ArchTool) { x.Name = t.Name }, c)
	case TargetSandbox:
		if err := need(t.Name, "name"); err != nil {
			return err
		}
		return applyItem(&r.Sandboxes, func(x ArchSandbox) bool { return x.Name == t.Name }, func(x *ArchSandbox) { x.Name = t.Name }, c)
	case TargetRouting:
		if err := need(t.Agent, "agent"); err != nil {
			return err
		}
		return applyItem(&r.Orchestration.Routing, func(x ArchRoute) bool { return x.Agent == t.Agent }, func(x *ArchRoute) { x.Agent = t.Agent }, c)
	case TargetHandoff:
		if err := need(t.From, "from"); err != nil {
			return err
		}
		if err := need(t.To, "to"); err != nil {
			return err
		}
		return applyItem(&r.Orchestration.Handoffs, func(x ArchHandoff) bool { return x.From == t.From && x.To == t.To }, func(x *ArchHandoff) { x.From, x.To = t.From, t.To }, c)
	}
	return fmt.Errorf("unknown target kind %q", t.Kind)
}

func need(v, field string) error {
	if v == "" {
		return fmt.Errorf("the target has no %s", field)
	}
	return nil
}

// decodeValue reads a change's value into v; a missing value is an error.
func decodeValue(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return fmt.Errorf("the change has no value")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("the value has the wrong shape: %w", err)
	}
	return nil
}

// applyItem sets or removes the item of list that match picks, appending a new
// one. key stamps the target's key onto the value, so a value cannot name a
// different item than its target.
func applyItem[T any](list *[]T, match func(T) bool, key func(*T), c ProposalChange) error {
	i := findIn(*list, match)
	if c.Op == OpRemove {
		if i >= 0 {
			*list = append((*list)[:i:i], (*list)[i+1:]...)
		}
		return nil
	}
	var item T
	if err := decodeValue(c.Value, &item); err != nil {
		return err
	}
	key(&item)
	if i >= 0 {
		(*list)[i] = item
	} else {
		*list = append(*list, item)
	}
	return nil
}

// applyLayer sets or removes a layer. A change keeps the layer's access.
func applyLayer(r *ArchitectureRecord, name string, c ProposalChange) error {
	i := findIn(r.Layers, func(l ArchLayer) bool { return l.Name == name })
	if c.Op != OpChange || i < 0 {
		return applyItem(&r.Layers, func(l ArchLayer) bool { return l.Name == name }, func(l *ArchLayer) { l.Name = name }, c)
	}
	var item ArchLayer
	if err := decodeValue(c.Value, &item); err != nil {
		return err
	}
	item.Name, item.Access = name, r.Layers[i].Access
	r.Layers[i] = item
	return nil
}

// applyAccess sets or removes one cell of an access matrix. The default cell
// (agent "*") can be set but not removed.
func applyAccess(a *LayerAccess, agent string, c ProposalChange) error {
	if err := need(agent, "agent"); err != nil {
		return err
	}
	if c.Op == OpRemove {
		if agent != DefaultAccessAgent {
			delete(a.Agents, agent)
		}
		return nil
	}
	var level AccessLevel
	if err := decodeValue(c.Value, &level); err != nil {
		return err
	}
	if !level.Valid() {
		return fmt.Errorf("access level %q is not none, read or change", level)
	}
	if agent == DefaultAccessAgent {
		a.Default = level
		return nil
	}
	if a.Agents == nil {
		a.Agents = map[string]AccessLevel{}
	}
	a.Agents[agent] = level
	return nil
}

func findIn[T any](list []T, match func(T) bool) int {
	for i, x := range list {
		if match(x) {
			return i
		}
	}
	return -1
}
