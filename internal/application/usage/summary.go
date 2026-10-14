package usage

import (
	"context"
	"sort"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// topN is how many tasks and sessions the summary ranks.
const topN = 10

// UsageSummary answers a project's usage over r: totals, tokens over time
// split by model, and the top tasks and sessions by tokens.
func (s *Service) UsageSummary(_ context.Context, projectID string, r ports.UsageRange) (*ports.UsageSummary, error) {
	if r == "" {
		r = ports.UsageToday
	}
	since, bucket, err := s.span(r)
	if err != nil {
		return nil, err
	}
	project := projectKey(projectID)
	groups, err := s.repo.Groups(ports.UsageGroupFilter{ProjectID: project, Since: since, LongAbove: longPromptAbove})
	if err != nil {
		return nil, err
	}
	points, err := s.repo.Series(project, since, bucket)
	if err != nil {
		return nil, err
	}
	sum := &ports.UsageSummary{Range: r, Since: since, Bucket: bucket, Series: make([]ports.UsageSeriesTokens, len(points))}
	tasks, sessions := map[string]*totaller{}, map[string]*totaller{}
	sessionTask := map[string]string{}
	var all totaller
	for _, g := range groups {
		all.add(g)
		if g.TaskID != "" {
			addTo(tasks, g.TaskID, g)
		}
		if g.SessionID != "" {
			addTo(sessions, g.SessionID, g)
			sessionTask[g.SessionID] = g.TaskID
		}
	}
	sum.Totals = all.totals()
	for i, p := range points {
		sum.Series[i] = ports.UsageSeriesTokens{Start: p.Start, Model: p.Model, InputTokens: p.InputTokens, OutputTokens: p.OutputTokens,
			CacheCreationInputTokens: p.CacheCreationInputTokens, CacheReadInputTokens: p.CacheReadInputTokens, TotalTokens: total(p.UsageSums)}
	}
	titles := s.titler()
	sum.TopTasks = []ports.UsageByTask{}
	for _, id := range ranked(tasks) {
		sum.TopTasks = append(sum.TopTasks, ports.UsageByTask{TaskID: id, TaskTitle: titles(id), Totals: tasks[id].totals()})
	}
	sum.TopSessions = []ports.UsageBySession{}
	for _, id := range ranked(sessions) {
		task := sessionTask[id]
		sum.TopSessions = append(sum.TopSessions, ports.UsageBySession{SessionID: id, TaskID: task, TaskTitle: titles(task), Totals: sessions[id].totals()})
	}
	return sum, nil
}

// UsageTotals answers one task's or one session's totals over all time.
func (s *Service) UsageTotals(_ context.Context, q ports.UsageTotalsQuery) (*ports.UsageTotals, error) {
	if (q.TaskID == "") == (q.SessionID == "") {
		return nil, invalid("name exactly one of task_id and session_id")
	}
	groups, err := s.repo.Groups(ports.UsageGroupFilter{TaskID: q.TaskID, SessionID: q.SessionID, LongAbove: longPromptAbove})
	if err != nil {
		return nil, err
	}
	var t totaller
	for _, g := range groups {
		t.add(g)
	}
	out := t.totals()
	return &out, nil
}

// span is where a range starts, and the bucket its series uses.
func (s *Service) span(r ports.UsageRange) (time.Time, ports.UsageBucket, error) {
	now := s.now().In(time.Local)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	switch r {
	case ports.UsageToday:
		return midnight, ports.UsageBucketHour, nil
	case ports.Usage7d:
		return midnight.AddDate(0, 0, -6), ports.UsageBucketDay, nil
	case ports.Usage30d:
		return midnight.AddDate(0, 0, -29), ports.UsageBucketDay, nil
	}
	return time.Time{}, "", invalid("range must be today, 7d or 30d")
}

// titler looks task titles up once each.
func (s *Service) titler() func(id string) string {
	titles := map[string]string{}
	return func(id string) string {
		if id == "" {
			return ""
		}
		title, ok := titles[id]
		if !ok {
			if t := s.readers.Tickets.Get(id); t != nil {
				title = t.Title
			}
			titles[id] = title
		}
		return title
	}
}

// totaller sums groups into totals, pricing each group by its model.
type totaller struct {
	ports.UsageSums
	cost     float64
	priced   bool
	unpriced int64
}

func (t *totaller) add(g ports.UsageGroup) {
	t.Requests += g.Requests
	t.InputTokens += g.InputTokens
	t.OutputTokens += g.OutputTokens
	t.CacheCreationInputTokens += g.CacheCreationInputTokens
	t.CacheReadInputTokens += g.CacheReadInputTokens
	if usd, ok := costOf(g.Model, g.Long, g.UsageSums); ok {
		t.cost += usd
		t.priced = true
	} else {
		t.unpriced += total(g.UsageSums)
	}
}

// totals are the sums with their estimated cost: nil only when there were
// tokens and none of them had a price.
func (t *totaller) totals() ports.UsageTotals {
	out := ports.UsageTotals{Requests: t.Requests, InputTokens: t.InputTokens, OutputTokens: t.OutputTokens,
		CacheCreationInputTokens: t.CacheCreationInputTokens, CacheReadInputTokens: t.CacheReadInputTokens,
		TotalTokens: total(t.UsageSums), UnpricedTokens: t.unpriced}
	if t.priced || t.unpriced == 0 {
		cost := t.cost
		out.EstimatedCostUSD = &cost
	}
	return out
}

func addTo(m map[string]*totaller, id string, g ports.UsageGroup) {
	t, ok := m[id]
	if !ok {
		t = &totaller{}
		m[id] = t
	}
	t.add(g)
}

// ranked are the topN ids by total tokens, most first.
func ranked(m map[string]*totaller) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := total(m[ids[i]].UsageSums), total(m[ids[j]].UsageSums)
		if a != b {
			return a > b
		}
		return ids[i] < ids[j]
	})
	if len(ids) > topN {
		ids = ids[:topN]
	}
	return ids
}

func total(s ports.UsageSums) int64 {
	r := domain.UsageRecord{InputTokens: s.InputTokens, OutputTokens: s.OutputTokens,
		CacheCreationInputTokens: s.CacheCreationInputTokens, CacheReadInputTokens: s.CacheReadInputTokens}
	return r.TotalTokens()
}
