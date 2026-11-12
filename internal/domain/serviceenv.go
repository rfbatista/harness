package domain

import (
	"sort"
	"strconv"
	"strings"
)

// A service's saved variables are dotenv text (ParseDotenv reads it). An agent
// sets and unsets them key by key (ADR 0083): these functions apply such a
// change to the text, and never put a value in an error.

// FormatDotenv writes variables as dotenv text that ParseDotenv reads back
// exactly: a plain value as it is, any other double-quoted and escaped.
func FormatDotenv(vars []EnvVar) string {
	var b strings.Builder
	for _, v := range vars {
		if plainDotenvValue(v.Value) {
			b.WriteString(v.Key + "=" + v.Value + "\n")
			continue
		}
		b.WriteString(v.Key + "=\"" + dotenvQuoted.Replace(v.Value) + "\"\n")
	}
	return b.String()
}

// plainDotenvValue says v reads back unquoted as itself: no space, quote,
// backslash, comment mark or line break, which an unquoted value would trim or
// cut.
func plainDotenvValue(v string) bool {
	return !strings.ContainsAny(v, " \t\r\n\"'\\#")
}

var dotenvQuoted = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// EnvValueProblem is why an agent's value of key cannot be saved, "" when it
// can: a NUL, a carriage return, and for an image service a newline.
func EnvValueProblem(kind ServiceKind, key, value string) string {
	switch {
	case strings.ContainsRune(value, 0):
		return "env value of " + strconv.Quote(key) + " contains a NUL character"
	case strings.ContainsRune(value, '\r'):
		return "env value of " + strconv.Quote(key) + " contains a carriage return"
	case kind == ServiceKindImage && strings.ContainsRune(value, '\n'):
		return "env value of " + strconv.Quote(key) + " contains a newline (image services)"
	}
	return ""
}

// ApplyEnvChanges sets and unsets variables of dotenv text. It answers the new
// text and each change as a service request lists it (previous_set says the key
// was set before). An unset of a key that is not set, and a set to the value
// the key already has, change nothing and are left out; when nothing changes the
// text comes back as it was. A bad key or value is INVALID_INPUT naming the key.
func ApplyEnvChanges(kind ServiceKind, current string, set map[string]string, unset []string) (string, []ServiceRequestEnv, error) {
	vars, _ := ParseDotenv(current)
	at := map[string]int{}
	for i, v := range vars {
		at[v.Key] = i
	}
	var changes []ServiceRequestEnv
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !ValidEnvKey(k) {
			return "", nil, invalidService("env key %s is not a valid variable name", strconv.Quote(k))
		}
		if msg := EnvValueProblem(kind, k, set[k]); msg != "" {
			return "", nil, invalidService("%s", msg)
		}
	}
	drop := map[string]bool{}
	for _, k := range unset {
		if !ValidEnvKey(k) {
			return "", nil, invalidService("env key %s is not a valid variable name", strconv.Quote(k))
		}
		drop[k] = true
	}
	for _, k := range keys {
		if i, ok := at[k]; ok {
			if vars[i].Value == set[k] {
				continue
			}
			vars[i].Value = set[k]
			changes = append(changes, ServiceRequestEnv{Name: k, Value: set[k], Change: "set", PreviousSet: true})
			continue
		}
		at[k] = len(vars)
		vars = append(vars, EnvVar{Key: k, Value: set[k]})
		changes = append(changes, ServiceRequestEnv{Name: k, Value: set[k], Change: "set"})
	}
	var unsetKeys []string
	for k := range drop {
		unsetKeys = append(unsetKeys, k)
	}
	sort.Strings(unsetKeys)
	for _, k := range unsetKeys {
		if _, ok := set[k]; ok {
			continue // set wins over unset of the same key
		}
		if i, ok := at[k]; ok && i >= 0 {
			changes = append(changes, ServiceRequestEnv{Name: k, Change: "unset", PreviousSet: true})
			at[k] = -1
		}
	}
	if len(changes) == 0 {
		return current, nil, nil
	}
	kept := vars[:0:0]
	for _, v := range vars {
		if at[v.Key] != -1 {
			kept = append(kept, v)
		}
	}
	return FormatDotenv(kept), changes, nil
}
