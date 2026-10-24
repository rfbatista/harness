package shell

// Helper says whether the writing helper can run, for every page that
// renders a magic pen (Frame.Helper). When it cannot, Reason says why and
// FixURL links to the page that fixes it; the pens then show "Writing helper
// unavailable". The server fills it from the readiness check behind
// WRITING_HELPER_UNAVAILABLE.
type Helper struct {
	Available      bool
	Reason, FixURL string
}
