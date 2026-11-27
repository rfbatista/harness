package tooling

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// WithDeclarations serves declare_done and run_gates, the Guard layer's tools
// (ADR 0012). Without it they answer that they are unavailable.
func WithDeclarations(d ports.Declarations) Option {
	return func(o *options) { o.declarations = d }
}

// sessionGateTools are declare_done and run_gates. A refused declaration is a
// coded refusal whose message carries every refusing check's reason; a gate
// run, red or green, is an answer.
func sessionGateTools(sessions ports.SessionRepository, decl ports.Declarations) []domain.Tool {
	session := func(ctx context.Context) (string, error) {
		id := SessionIDFrom(ctx)
		if id == "" || sessions.Get(id) == nil {
			return "", &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no such session"}
		}
		if decl == nil {
			return "", &domain.StructuredError{Code: "UNAVAILABLE", Message: "gate runs are not available on this server"}
		}
		return id, nil
	}
	return []domain.Tool{{
		Name: "declare_done",
		Description: "Declare your work done. The harness runs the repository's gates in your worktree, in your sandbox, and any " +
			"other declaration check; green accepts the declaration, and the answer says so. A refusal is coded and carries every " +
			"reason: GATES_RED with each failing gate's command, exit code, duration and the end of its output, " +
			"REQUIRED_GATE_MISSING naming the kinds a required gate has no command for. Fix what it names and declare again. A " +
			"session that is not gated is accepted without a run. Use run_gates first to see a failure without declaring.",
		InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
		Source:      "code",
		Handler: func(ctx context.Context, _ map[string]any) (any, error) {
			id, err := session(ctx)
			if err != nil {
				return nil, err
			}
			res, err := decl.Declare(ctx, id)
			if err != nil {
				return nil, err
			}
			if !res.Accepted {
				return nil, &domain.StructuredError{Code: res.Refusals[0].Code, Message: res.Message()}
			}
			out := map[string]any{"accepted": true}
			if res.Run != nil {
				out["run"] = gateRunAnswer(res.Run)
			} else {
				out["note"] = "this session is not gated: nothing ran"
			}
			return out, nil
		},
	}, {
		Name: "run_gates",
		Description: "Practice before you declare: run the repository's gates in your worktree, in your sandbox, as the harness runs them " +
			"for a declaration, and answer each gate's command, exit code, duration and output tail. Give kinds (for example [\"test\"]) " +
			"to run only those gates; leave it out to run them all. The run is recorded as a practice run and never counts toward the " +
			"retry limit. A session that is not gated is refused with SESSION_NOT_GATED.",
		InputSchema: schemaFromJSON(`{"type":"object","properties":{` +
			`"kinds":{"type":"array","items":{"type":"string"},"description":"Gate kinds to run, such as build, test, lint, secret_scan, lockfile, typecheck, sast, dependency_scan. Default: every gate."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			id, err := session(ctx)
			if err != nil {
				return nil, err
			}
			var kinds []domain.GateKind
			for _, k := range getStringSlice(args, "kinds") {
				kind := domain.GateKind(strings.TrimSpace(k))
				if !slices.Contains(domain.RequiredGateKinds, kind) && !slices.Contains(domain.OptionalGateKinds, kind) {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "unknown gate kind " + strconv.Quote(k)}
				}
				kinds = append(kinds, kind)
			}
			run, err := decl.Practice(ctx, id, kinds)
			if err != nil {
				return nil, err
			}
			return gateRunAnswer(run), nil
		},
	}}
}

// gateRunAnswer is a gate run as an agent reads it.
func gateRunAnswer(run *domain.GateRun) map[string]any {
	gates := make([]map[string]any, 0, len(run.Gates))
	for _, g := range run.Gates {
		gates = append(gates, map[string]any{
			"kind": g.Kind, "command": g.Command, "exit_code": g.ExitCode, "timed_out": g.TimedOut,
			"duration_ms": g.DurationMs, "passed": g.Passed(), "output": g.OutputTail(domain.MaxReportedOutputBytes),
		})
	}
	out := map[string]any{"run_id": run.ID, "trigger": run.Trigger, "result": run.Result, "counted": run.Counted, "gates": gates}
	if len(run.MissingKinds) > 0 {
		out["missing_kinds"] = run.MissingKinds
	}
	if len(run.Kinds) > 0 {
		out["kinds"] = run.Kinds
	}
	return out
}
