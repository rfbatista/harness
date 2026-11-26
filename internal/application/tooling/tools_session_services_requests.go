package tooling

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// create_service and update_service (ADR 0083): an agent asks to define or
// change a service, and the call waits for the person's answer.

const createSchema = `{"type":"object","required":["name","kind","reason"],"properties":{` +
	`"name":{"type":"string","description":"The new service's name: lowercase letters, digits, '-', '_' or '.'."},` +
	`"kind":{"type":"string","enum":["command","image"]},` +
	`"repository":{"type":"string","description":"The repository's name, when the service belongs to one."},` +
	`"export_repository_env":{"type":"boolean","description":"Give the run the repository's saved root .env."},` +
	`"env":{"type":"object","additionalProperties":{"type":"string"},"description":"Saved variables of the service."},` +
	`"credentials":{"type":"object","additionalProperties":{"type":"string"},"description":"Access credentials for the person to log into what the service runs, such as a test account: label to value. The person sees them masked; you read back the labels only."},` +
	`"command":{"type":"object","description":"A command service: line, subdir (relative, inside the repository), ports (informational).","properties":{"line":{"type":"string"},"subdir":{"type":"string"},"ports":{"type":"array","items":{"type":"integer"}}}},` +
	`"image":{"type":"object","description":"An image service: ref, args (replace the image's command), ports (host to container).","properties":{"ref":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"ports":{"type":"array","items":{"type":"object","properties":{"host":{"type":"integer"},"container":{"type":"integer"},"protocol":{"type":"string","enum":["tcp","udp"]}}}}}},` +
	`"reason":{"type":"string","description":"Why you ask: the person reads it when deciding."}}}`

const updateSchema = `{"type":"object","required":["name","changes","reason"],"properties":{` +
	`"name":{"type":"string","description":"The service's name, as list_services gives it."},` +
	`"changes":{"type":"object","description":"What to change; leave out what stays. env: set adds or replaces saved variables, unset removes them.","properties":{` +
	`"name":{"type":"string"},"repository":{"type":"string"},"export_repository_env":{"type":"boolean"},` +
	`"command":{"type":"object","properties":{"line":{"type":"string"},"subdir":{"type":"string"},"ports":{"type":"array","items":{"type":"integer"}}}},` +
	`"image":{"type":"object","properties":{"ref":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"ports":{"type":"array","items":{"type":"object","properties":{"host":{"type":"integer"},"container":{"type":"integer"},"protocol":{"type":"string"}}}}}},` +
	`"env":{"type":"object","properties":{"set":{"type":"object","additionalProperties":{"type":"string"}},"unset":{"type":"array","items":{"type":"string"}}}},` +
	`"credentials":{"type":"object","description":"Access credentials (label to value): set adds or replaces, unset removes by label. You read back labels only.","properties":{"set":{"type":"object","additionalProperties":{"type":"string"}},"unset":{"type":"array","items":{"type":"string"}}}}}},` +
	`"reason":{"type":"string","description":"Why you ask: the person reads it when deciding."}}}`

func (t *servicesTools) definitionTools() []domain.Tool {
	return []domain.Tool{
		{
			Name: "create_service",
			Description: "Request a new service in this project: a command line or a container image, with its repository and saved variables. " +
				"It asks the person: the call waits, with no time limit, until they approve or deny; reason says why. On approval the service is " +
				"created, stopped, and the answer is its ServiceView. Bad arguments are refused at once and nothing is asked.",
			InputSchema: schemaFromJSON(createSchema),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := t.scope(ctx)
				if err != nil {
					return nil, err
				}
				in, err := t.createInput(ctx, sess, args)
				if err != nil {
					return nil, err
				}
				if err := requireReason(args); err != nil {
					return nil, err
				}
				in, err = t.services.ValidateServiceInput(ctx, sess.ProjectID, "", in)
				if err != nil {
					return nil, t.refusal(ctx, err, in.Name)
				}
				out, err := t.submitDefinition(ctx, sess, ports.ServiceRequestInput{
					SessionID: sess.ID, Action: domain.ServiceRequestCreate, ServiceName: in.Name, Reason: getString(args, "reason", ""),
					Summary: t.createSummary(ctx, in), Proposed: &in, EnvChanges: envSets(in.Env), CredentialChanges: credentialSets(in.Credentials),
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"service": t.view(ctx, out.Service, sess.ID)}, nil
			},
		},
		{
			Name: "update_service",
			Description: "Request changes to a service's definition, including its saved variables (env: set adds or replaces, unset removes), " +
				"for any service of this project. It asks the person: the call waits, with no time limit, until they approve or deny; reason says why. " +
				"A change that changes nothing is answered at once. The answer holds the service and restart_needed, true when the service is running " +
				"and picks the change up at its next start. You see the values of variables you set; others stay hidden.",
			InputSchema: schemaFromJSON(updateSchema),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, svc, err := t.target(ctx, args)
				if err != nil {
					return nil, err
				}
				if err := requireReason(args); err != nil {
					return nil, err
				}
				after, changes, envChanges, credChanges, err := t.applyChanges(ctx, sess, svc, args)
				if err != nil {
					return nil, err
				}
				if len(changes) == 0 && len(envChanges) == 0 && len(credChanges) == 0 {
					return map[string]any{"service": t.view(ctx, svc, sess.ID), "restart_needed": false}, nil
				}
				after, err = t.services.ValidateServiceInput(ctx, sess.ProjectID, svc.ID, after)
				if err != nil {
					return nil, t.refusal(ctx, err, after.Name)
				}
				out, err := t.submitDefinition(ctx, sess, ports.ServiceRequestInput{
					SessionID: sess.ID, Action: domain.ServiceRequestUpdate, ServiceID: svc.ID, ServiceName: svc.Name, Reason: getString(args, "reason", ""),
					Summary: updateSummary(svc.Name, changes, envChanges, credChanges), Proposed: &after,
					Changes: append(append(changes, envFieldChanges(envChanges)...), credentialFieldChanges(credChanges)...), EnvChanges: envChanges, CredentialChanges: credChanges,
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"service": t.view(ctx, out.Service, sess.ID), "restart_needed": out.RestartNeeded}, nil
			},
		},
	}
}

func requireReason(args map[string]any) error {
	if strings.TrimSpace(getString(args, "reason", "")) == "" {
		return invalidInput("reason is required: say why you ask, the person reads it")
	}
	return nil
}

// submitDefinition files a create or an update, and waits for the answer.
func (t *servicesTools) submitDefinition(ctx context.Context, sess *domain.Session, in ports.ServiceRequestInput) (*ports.ServiceRequestOutcome, error) {
	if t.requests == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "service requests are not available on this server"}
	}
	return t.requests.Submit(ctx, in)
}

// repositoryByName is the id of the project's repository of that name.
func (t *servicesTools) repositoryByName(ctx context.Context, projectID, name string) (string, error) {
	list, err := t.repos.ListRepositories(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, r := range list {
		if r.Name == name {
			return r.ID, nil
		}
	}
	return "", &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "no repository named " + strconv.Quote(name) + " in this project"}
}

// createInput reads create_service's arguments into the definition.
func (t *servicesTools) createInput(ctx context.Context, sess *domain.Session, args map[string]any) (domain.ServiceInput, error) {
	in := domain.ServiceInput{Name: getString(args, "name", ""), Kind: domain.ServiceKind(getString(args, "kind", "")), ExportRepositoryEnv: getBool(args, "export_repository_env", false)}
	if name := strings.TrimSpace(getString(args, "repository", "")); name != "" {
		id, err := t.repositoryByName(ctx, sess.ProjectID, name)
		if err != nil {
			return in, err
		}
		in.RepositoryID = id
	}
	var err error
	if in.Env, err = envText(in.Kind, args["env"]); err != nil {
		return in, err
	}
	if in.Credentials, err = credentialsOf(args["credentials"]); err != nil {
		return in, err
	}
	if in.Command, err = decodeObject[domain.ServiceCommand](args, "command"); err != nil {
		return in, err
	}
	if in.Image, err = decodeObject[domain.ServiceImage](args, "image"); err != nil {
		return in, err
	}
	return in, nil
}

// decodeObject reads an object argument into T; nil when it is absent.
func decodeObject[T any](args map[string]any, key string) (*T, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, invalidInput(key + " must be an object")
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, invalidInput(key + ": " + err.Error())
	}
	return &out, nil
}

// envText turns env's map of variables into the dotenv text a service saves.
func envText(kind domain.ServiceKind, raw any) (string, error) {
	if raw == nil {
		return "", nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return "", invalidInput("env must be an object of variable names to string values")
	}
	set := make(map[string]string, len(m))
	for k, v := range m {
		str, ok := v.(string)
		if !ok {
			return "", invalidInput("env value of " + strconv.Quote(k) + " must be a string")
		}
		set[k] = str
	}
	text, _, err := domain.ApplyEnvChanges(kind, "", set, nil)
	return text, err
}

// credentialsOf reads the credentials argument, label to value, in label order;
// nil when it is absent.
func credentialsOf(raw any) ([]domain.ServiceCredential, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, invalidInput("credentials must be an object of labels to string values")
	}
	labels := make([]string, 0, len(m))
	for k := range m {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	out := make([]domain.ServiceCredential, 0, len(m))
	for _, k := range labels {
		v, ok := m[k].(string)
		if !ok {
			return nil, invalidInput("credential " + strconv.Quote(k) + " must be a string")
		}
		out = append(out, domain.ServiceCredential{Label: k, Value: v})
	}
	return out, nil
}

// credentialSets lists each credential as one the request sets.
func credentialSets(creds []domain.ServiceCredential) []domain.ServiceRequestCredential {
	out := make([]domain.ServiceRequestCredential, 0, len(creds))
	for _, c := range creds {
		out = append(out, domain.ServiceRequestCredential{Label: c.Label, Value: c.Value, Change: "set"})
	}
	return out
}

// envSets lists each variable of dotenv text as one the request sets.
func envSets(text string) []domain.ServiceRequestEnv {
	vars, _ := domain.ParseDotenv(text)
	out := make([]domain.ServiceRequestEnv, 0, len(vars))
	for _, v := range vars {
		out = append(out, domain.ServiceRequestEnv{Name: v.Key, Value: v.Value, Change: "set"})
	}
	return out
}

// createSummary is the one line the person reads.
func (t *servicesTools) createSummary(ctx context.Context, in domain.ServiceInput) string {
	what := ""
	switch {
	case in.Command != nil:
		what = in.Command.Line
	case in.Image != nil:
		what = in.Image.Ref
	}
	text := "create " + string(in.Kind) + " service " + in.Name + ": " + what
	if repo := t.repositoryName(ctx, in.RepositoryID); repo != "" {
		text += " in " + repo
	}
	return text
}

func updateSummary(name string, changes []domain.ServiceRequestChange, env []domain.ServiceRequestEnv, creds []domain.ServiceRequestCredential) string {
	var parts []string
	for _, c := range changes {
		parts = append(parts, c.Field)
	}
	for _, e := range env {
		parts = append(parts, "env."+e.Name)
	}
	for _, c := range creds {
		parts = append(parts, "credentials."+c.Label)
	}
	return "update service " + name + ": " + strings.Join(parts, ", ")
}

// envFieldChanges lists the variables of an update as changes with no value.
func envFieldChanges(env []domain.ServiceRequestEnv) []domain.ServiceRequestChange {
	out := make([]domain.ServiceRequestChange, 0, len(env))
	for _, e := range env {
		out = append(out, domain.ServiceRequestChange{Field: "env." + e.Name})
	}
	return out
}

// credentialFieldChanges lists the credentials of an update as changes with
// no value.
func credentialFieldChanges(creds []domain.ServiceRequestCredential) []domain.ServiceRequestChange {
	out := make([]domain.ServiceRequestChange, 0, len(creds))
	for _, c := range creds {
		out = append(out, domain.ServiceRequestChange{Field: "credentials." + c.Label})
	}
	return out
}

// applyCredentialChanges is the credentials after set and unset, kept in
// order with new labels last, and what changed.
func applyCredentialChanges(cur []domain.ServiceCredential, raw map[string]any) ([]domain.ServiceCredential, []domain.ServiceRequestCredential, error) {
	set, err := credentialsOf(raw["set"])
	if err != nil {
		return nil, nil, err
	}
	var unset []string
	if l, ok := raw["unset"].([]any); ok {
		for _, k := range l {
			if str, ok := k.(string); ok {
				unset = append(unset, str)
			}
		}
	}
	after := append([]domain.ServiceCredential{}, cur...)
	var changes []domain.ServiceRequestCredential
	for _, label := range unset {
		for i, c := range after {
			if c.Label == label {
				after = append(after[:i], after[i+1:]...)
				changes = append(changes, domain.ServiceRequestCredential{Label: label, Change: "unset"})
				break
			}
		}
	}
	for _, c := range set {
		found := false
		for i := range after {
			if after[i].Label == c.Label {
				found = true
				if after[i].Value != c.Value {
					after[i].Value = c.Value
					changes = append(changes, domain.ServiceRequestCredential{Label: c.Label, Value: c.Value, Change: "set"})
				}
			}
		}
		if !found {
			after = append(after, c)
			changes = append(changes, domain.ServiceRequestCredential{Label: c.Label, Value: c.Value, Change: "set"})
		}
	}
	return after, changes, nil
}

// applyChanges is the service's definition after the changes, and what changed.
func (t *servicesTools) applyChanges(ctx context.Context, sess *domain.Session, svc *domain.Service, args map[string]any) (domain.ServiceInput, []domain.ServiceRequestChange, []domain.ServiceRequestEnv, []domain.ServiceRequestCredential, error) {
	after := domain.ServiceInput{
		Name: svc.Name, Kind: svc.Kind, RepositoryID: svc.RepositoryID, ExportRepositoryEnv: svc.ExportRepositoryEnv, Env: svc.Env,
	}
	if svc.Command != nil {
		c := *svc.Command
		c.Ports = append([]int{}, c.Ports...)
		after.Command = &c
	}
	if svc.Image != nil {
		i := *svc.Image
		i.Args = append([]string{}, i.Args...)
		i.Ports = append([]domain.ServicePort{}, i.Ports...)
		after.Image = &i
	}
	raw, _ := args["changes"].(map[string]any)
	if args["changes"] != nil && raw == nil {
		return after, nil, nil, nil, invalidInput("changes must be an object")
	}
	var changes []domain.ServiceRequestChange
	note := func(field, before, now string) {
		if before != now {
			changes = append(changes, domain.ServiceRequestChange{Field: field, Before: before, After: now})
		}
	}
	if v, ok := raw["name"].(string); ok {
		note("name", after.Name, strings.TrimSpace(v))
		after.Name = strings.TrimSpace(v)
	}
	if v, ok := raw["repository"].(string); ok {
		before := t.repositoryName(ctx, after.RepositoryID)
		id := ""
		if strings.TrimSpace(v) != "" {
			var err error
			if id, err = t.repositoryByName(ctx, sess.ProjectID, strings.TrimSpace(v)); err != nil {
				return after, nil, nil, nil, err
			}
		}
		after.RepositoryID = id
		note("repository", before, t.repositoryName(ctx, id))
	}
	if v, ok := raw["export_repository_env"].(bool); ok {
		note("export_repository_env", strconv.FormatBool(after.ExportRepositoryEnv), strconv.FormatBool(v))
		after.ExportRepositoryEnv = v
	}
	if obj, err := decodeObject[domain.ServiceCommand](raw, "command"); err != nil {
		return after, nil, nil, nil, err
	} else if obj != nil {
		if svc.Kind != domain.ServiceKindCommand {
			return after, nil, nil, nil, invalidInput("command: " + svc.Name + " is an image service")
		}
		cmd := after.Command
		sub, _ := raw["command"].(map[string]any)
		if _, ok := sub["line"]; ok {
			note("command.line", cmd.Line, strings.TrimSpace(obj.Line))
			cmd.Line = obj.Line
		}
		if _, ok := sub["subdir"]; ok {
			note("command.subdir", cmd.Subdir, strings.TrimSpace(obj.Subdir))
			cmd.Subdir = obj.Subdir
		}
		if _, ok := sub["ports"]; ok {
			note("command.ports", intsText(cmd.Ports), intsText(obj.Ports))
			cmd.Ports = obj.Ports
		}
	}
	if obj, err := decodeObject[domain.ServiceImage](raw, "image"); err != nil {
		return after, nil, nil, nil, err
	} else if obj != nil {
		if svc.Kind != domain.ServiceKindImage {
			return after, nil, nil, nil, invalidInput("image: " + svc.Name + " is a command service")
		}
		img := after.Image
		sub, _ := raw["image"].(map[string]any)
		if _, ok := sub["ref"]; ok {
			note("image.ref", img.Ref, strings.TrimSpace(obj.Ref))
			img.Ref = obj.Ref
		}
		if _, ok := sub["args"]; ok {
			note("image.args", strings.Join(img.Args, " "), strings.Join(obj.Args, " "))
			img.Args = obj.Args
		}
		if _, ok := sub["ports"]; ok {
			note("image.ports", portsText(img.Ports), portsText(obj.Ports))
			img.Ports = obj.Ports
		}
	}
	var envChanges []domain.ServiceRequestEnv
	if envRaw, ok := raw["env"].(map[string]any); ok {
		set := map[string]string{}
		if m, ok := envRaw["set"].(map[string]any); ok {
			for k, v := range m {
				str, ok := v.(string)
				if !ok {
					return after, nil, nil, nil, invalidInput("env value of " + strconv.Quote(k) + " must be a string")
				}
				set[k] = str
			}
		}
		var unset []string
		if l, ok := envRaw["unset"].([]any); ok {
			for _, k := range l {
				if str, ok := k.(string); ok {
					unset = append(unset, str)
				}
			}
		}
		text, ch, err := domain.ApplyEnvChanges(svc.Kind, svc.Env, set, unset)
		if err != nil {
			return after, nil, nil, nil, err
		}
		after.Env, envChanges = text, ch
	}
	var credChanges []domain.ServiceRequestCredential
	if credRaw, ok := raw["credentials"].(map[string]any); ok {
		creds, ch, err := applyCredentialChanges(svc.Credentials, credRaw)
		if err != nil {
			return after, nil, nil, nil, err
		}
		after.Credentials, credChanges = creds, ch
	}
	return after, changes, envChanges, credChanges, nil
}

func intsText(l []int) string {
	parts := make([]string, len(l))
	for i, n := range l {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func portsText(l []domain.ServicePort) string {
	parts := make([]string, len(l))
	for i, p := range l {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		parts[i] = strconv.Itoa(p.Host) + "->" + strconv.Itoa(p.Container) + "/" + proto
	}
	return strings.Join(parts, ", ")
}

// requestState is what a ServiceView shows of the service's requests: the saved
// variables this session's approved requests set (while their saved value is
// still the one it set), and this session's own pending request.
func (t *servicesTools) requestState(ctx context.Context, svc *domain.Service, saved []domain.EnvVar, sessionID string) (map[string]string, *pendingRequestView) {
	values := map[string]string{}
	if t.requests == nil {
		return values, nil
	}
	list, err := t.requests.List(ctx, domain.ServiceRequestFilter{ServiceID: svc.ID})
	if err != nil {
		return values, nil
	}
	var pending *pendingRequestView
	// Oldest first: the last request that touched a key decides it.
	owned := map[string]string{}
	for i := len(list) - 1; i >= 0; i-- {
		r := list[i]
		switch {
		case r.State == domain.ServiceRequestPending && r.SessionID == sessionID && pending == nil:
			pending = &pendingRequestView{ID: r.ID, Action: string(r.Action)}
		case r.State == domain.ServiceRequestDone && (r.Action == domain.ServiceRequestCreate || r.Action == domain.ServiceRequestUpdate):
			for _, e := range r.Env {
				if e.Change == "unset" || r.SessionID != sessionID {
					delete(owned, e.Name)
					continue
				}
				owned[e.Name] = e.Value
			}
		}
	}
	for _, v := range saved {
		if want, ok := owned[v.Key]; ok && want == v.Value {
			values[v.Key] = v.Value
		}
	}
	return values, pending
}
