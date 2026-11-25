package tooling

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ProductOwnerSource adds a source to ProductOwnerSources.
type ProductOwnerSource func(*ProductOwnerSources)

// WithDigests gives the product owner's tools the project's digests (ADR 0113).
func WithDigests(d ports.ProductOwnerDigests) ProductOwnerSource {
	return func(s *ProductOwnerSources) { s.Digests = d }
}

const digestDay = "2006-01-02"

// maxDigestsRead bounds how many digests one read answers.
const maxDigestsRead = 31

// productOwnerDigestReads is the product owner's read of the daily digests of
// its own project, newest first. A digest is the record of one day: asked
// about the past, the product owner answers from it.
func productOwnerDigestReads(src ProductOwnerSources) []domain.Tool {
	if src.Digests == nil {
		return nil
	}
	return []domain.Tool{{
		Name: "list_product_owner_digests",
		Description: "Read the daily digests of this project, newest first. A digest is the record of what moved in the project " +
			"over the period it covers: tasks that moved status (from and to), sessions that finished or failed, and what waited " +
			"on the person, each with its link, and a short summary. Use it to answer about the past (\"what happened last Tuesday?\"), " +
			"and say that the answer comes from the digest. Give `date` for one day, or `from` and `to` for a range; with neither, " +
			"the newest digests. A day with no digest was a quiet day or had no digest time set.",
		InputSchema: schemaFromJSON(`{"type":"object","properties":{` +
			`"date":{"type":"string","description":"One day, YYYY-MM-DD."},` +
			`"from":{"type":"string","description":"First day of a range, YYYY-MM-DD."},` +
			`"to":{"type":"string","description":"Last day of a range, YYYY-MM-DD. Default: the newest."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			sess, err := resolveProjectScope(ctx, src.Sessions)
			if err != nil {
				return nil, err
			}
			from, to := getString(args, "from", ""), getString(args, "to", "")
			if d := getString(args, "date", ""); d != "" {
				from, to = d, d
			}
			for _, d := range []string{from, to} {
				if d == "" {
					continue
				}
				if _, err := time.Parse(digestDay, d); err != nil {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "INVALID_INPUT: dates are YYYY-MM-DD, got " + d}
				}
			}
			before := ""
			if to != "" {
				last, _ := time.Parse(digestDay, to)
				before = last.AddDate(0, 0, 1).Format(digestDay)
			}
			out := []domain.ProductOwnerDigest{}
			for len(out) < maxDigestsRead {
				page, more, err := src.Digests.Digests(ctx, sess.ProjectID, before)
				if err != nil {
					return nil, err
				}
				for _, d := range page {
					if from != "" && d.Date < from {
						more = false
						break
					}
					if out = append(out, d); len(out) == maxDigestsRead {
						break
					}
				}
				if !more || len(page) == 0 || from == "" {
					break
				}
				before = page[len(page)-1].Date
			}
			return map[string]any{"count": len(out), "digests": out}, nil
		},
	}}
}
