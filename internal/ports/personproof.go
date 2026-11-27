package ports

import "context"

// The person credential (ADR 0016, Glossary "Person credential"): positive proof
// that a request comes from the person, held by their browser or client and by
// no session.

// PersonCredentials keeps the hashes of the credentials issued. The harness
// stores a hash only, so reading the store gives no credential.
type PersonCredentials interface {
	Add(ctx context.Context, hash string) error
	Has(ctx context.Context, hash string) (bool, error)
	Count(ctx context.Context) (int, error)
}

// PersonVerifier says whether a presented credential is one the harness issued.
type PersonVerifier interface {
	Verify(ctx context.Context, credential string) bool
}

// PersonPairing issues credentials: a one-time pairing code, held in memory and
// printed where only the person sees it, is redeemed once for a credential.
type PersonPairing interface {
	// Redeem checks the code, burns it and answers a new credential; a wrong,
	// used or expired code answers PAIRING_CODE_INVALID.
	Redeem(ctx context.Context, code string) (credential string, err error)
	// NewCode makes a pairing code for another browser or client; the previous
	// one stops working.
	NewCode() string
}

type personClientKey struct{}

// WithPersonClient says what kind of client is redeeming a pairing code
// ("browser" or "client"), for the notice the person gets of every credential
// issued.
func WithPersonClient(ctx context.Context, kind string) context.Context {
	return context.WithValue(ctx, personClientKey{}, kind)
}

// PersonClientFrom is the kind WithPersonClient set, "unknown" without one.
func PersonClientFrom(ctx context.Context) string {
	if k, _ := ctx.Value(personClientKey{}).(string); k != "" {
		return k
	}
	return "unknown"
}

// PersonRefusal is a call to a person-only route that carried no valid
// credential. It names no session: a caller that is not the person has none to
// prove.
type PersonRefusal struct {
	Method, Route string
	// ProjectID is the project the route names, when it names one.
	ProjectID string
	// SessionID is the session the route names (a /sessions/:id route), the one
	// the call is about, not who made it: the caller proved no identity.
	SessionID string
}

// PersonRefusals is told of each refusal, to log it and tell the person.
type PersonRefusals interface {
	Refused(ctx context.Context, r PersonRefusal)
}
