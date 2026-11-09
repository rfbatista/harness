package sqlite

import (
	"encoding/json"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// InterviewModel is the GORM model for domain.Interview. The questions and
// the answers are stored as JSON; times are Unix milliseconds, 0 for none,
// and Answers is "" for none.
type InterviewModel struct {
	ID          string  `gorm:"primaryKey"`
	SessionID   string  `gorm:"column:session_id;index:idx_interviews_session_state"`
	AgentName   string  `gorm:"column:agent_name"`
	Title       string  `gorm:"column:title"`
	AskedAt     int64   `gorm:"column:asked_at"`
	State       string  `gorm:"column:state;index:idx_interviews_session_state"`
	EndedAt     int64   `gorm:"column:ended_at"`
	Delivery    string  `gorm:"column:delivery"`
	DeliveredAt int64   `gorm:"column:delivered_at"`
	Questions   string  `gorm:"column:questions"`
	Answers     string  `gorm:"column:answers"`
	DismissNote *string `gorm:"column:dismiss_note"`
}

func (InterviewModel) TableName() string { return "interviews" }

// InterviewSeqModel is the number a session's interview feed has reached.
type InterviewSeqModel struct {
	SessionID string `gorm:"primaryKey;column:session_id"`
	Seq       int64  `gorm:"column:seq"`
}

func (InterviewSeqModel) TableName() string { return "interview_seqs" }

func interviewModel(i *domain.Interview) (*InterviewModel, error) {
	qs, err := json.Marshal(i.Questions)
	if err != nil {
		return nil, err
	}
	m := &InterviewModel{
		ID: i.ID, SessionID: i.SessionID, AgentName: i.AgentName, Title: i.Title,
		AskedAt: i.AskedAt.UnixMilli(), State: string(i.State), EndedAt: milliOf(i.EndedAt),
		DeliveredAt: milliOf(i.DeliveredAt), Questions: string(qs), DismissNote: i.DismissNote,
	}
	if i.Delivery != nil {
		m.Delivery = string(*i.Delivery)
	}
	if i.Answers != nil {
		as, err := json.Marshal(i.Answers)
		if err != nil {
			return nil, err
		}
		m.Answers = string(as)
	}
	return m, nil
}

func (m *InterviewModel) toDomain() *domain.Interview {
	i := &domain.Interview{
		ID: m.ID, SessionID: m.SessionID, AgentName: m.AgentName, Title: m.Title,
		AskedAt: time.UnixMilli(m.AskedAt), State: domain.InterviewState(m.State),
		EndedAt: timeFromMilli(m.EndedAt), DeliveredAt: timeFromMilli(m.DeliveredAt), DismissNote: m.DismissNote,
		Questions: []domain.InterviewQuestion{},
	}
	_ = json.Unmarshal([]byte(m.Questions), &i.Questions)
	if m.Delivery != "" {
		d := domain.InterviewDelivery(m.Delivery)
		i.Delivery = &d
	}
	if m.Answers != "" {
		_ = json.Unmarshal([]byte(m.Answers), &i.Answers)
	}
	return i
}

// InterviewRepository implements ports.InterviewRepository.
type InterviewRepository struct{ db *gorm.DB }

var _ ports.InterviewRepository = (*InterviewRepository)(nil)

func NewInterviewRepository(db *gorm.DB) *InterviewRepository { return &InterviewRepository{db: db} }

// nextSeq counts the session's feed up by one, inside tx.
func nextSeq(tx *gorm.DB, sessionID string) (int64, error) {
	row := InterviewSeqModel{SessionID: sessionID, Seq: 1}
	err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.Assignments(map[string]any{"seq": gorm.Expr("seq + 1")}),
	}).Create(&row).Error
	if err != nil {
		return 0, err
	}
	var seq int64
	err = tx.Model(&InterviewSeqModel{}).Select("seq").Where("session_id = ?", sessionID).Scan(&seq).Error
	return seq, err
}

func (r *InterviewRepository) Create(i *domain.Interview) (int64, error) {
	m, err := interviewModel(i)
	if err != nil {
		return 0, err
	}
	var seq int64
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		seq, err = nextSeq(tx, i.SessionID)
		return err
	})
	return seq, err
}

func (r *InterviewRepository) Update(i *domain.Interview) (int64, error) {
	m, err := interviewModel(i)
	if err != nil {
		return 0, err
	}
	var seq int64
	err = r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&InterviewModel{}).Where("id = ?", i.ID).Select("*").Omit("id", "session_id").Updates(m)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return interviewNotFound(i.ID)
		}
		seq, err = nextSeq(tx, i.SessionID)
		return err
	})
	return seq, err
}

func (r *InterviewRepository) Get(id string) (*domain.Interview, error) {
	var rows []InterviewModel
	if err := r.db.Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, interviewNotFound(id)
	}
	return rows[0].toDomain(), nil
}

func interviewNotFound(id string) error {
	return &domain.StructuredError{Code: "INTERVIEW_NOT_FOUND", Message: "interview " + id + " not found"}
}

func (r *InterviewRepository) Close(i *domain.Interview) (bool, int64, error) {
	m, err := interviewModel(i)
	if err != nil {
		return false, 0, err
	}
	var (
		seq int64
		won bool
	)
	err = r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&InterviewModel{}).Where("id = ? AND state = ?", i.ID, string(domain.InterviewOpen)).
			Select("*").Omit("id", "session_id").Updates(m)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		won = true
		seq, err = nextSeq(tx, i.SessionID)
		return err
	})
	if err == nil && !won {
		_, err = r.Get(i.ID) // a miss because the interview is not there
	}
	return won, seq, err
}

// The open interviews of the sessions, and for each session its newest closed
// ones up to the cap, in one query.
const interviewsOf = `SELECT id, session_id, agent_name, title, asked_at, state, ended_at, delivery, delivered_at, questions, answers, dismiss_note
FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY session_id, state = 'open' ORDER BY ended_at DESC, id DESC) AS n
      FROM interviews WHERE session_id IN ?)
WHERE state = 'open' OR n <= ?`

func (r *InterviewRepository) List(sessionIDs []string) (map[string]*domain.InterviewList, error) {
	out := make(map[string]*domain.InterviewList, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	// The feed number is read first: an interview that arrives between the
	// two reads is then in the list and above the number, which a follower
	// handles by replacing it by id.
	var seqs []InterviewSeqModel
	if err := r.db.Where("session_id IN ?", sessionIDs).Find(&seqs).Error; err != nil {
		return nil, err
	}
	for _, id := range sessionIDs {
		out[id] = &domain.InterviewList{Interviews: []*domain.Interview{}}
	}
	for _, s := range seqs {
		out[s.SessionID].Seq = s.Seq
	}
	var rows []InterviewModel
	if err := r.db.Raw(interviewsOf, sessionIDs, domain.MaxClosedInterviews).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		l := out[rows[i].SessionID]
		l.Interviews = append(l.Interviews, rows[i].toDomain())
	}
	for _, l := range out {
		sort.SliceStable(l.Interviews, func(a, b int) bool { return interviewBefore(l.Interviews[a], l.Interviews[b]) })
	}
	return out, nil
}

// interviewBefore orders the open ones first, oldest first, then the closed
// ones, the last to end first.
func interviewBefore(a, b *domain.Interview) bool {
	switch {
	case a.State.Closed() != b.State.Closed():
		return !a.State.Closed()
	case !a.State.Closed():
		if !a.AskedAt.Equal(b.AskedAt) {
			return a.AskedAt.Before(b.AskedAt)
		}
		return a.ID < b.ID
	}
	if ae, be := endedOf(a), endedOf(b); !ae.Equal(be) {
		return ae.After(be)
	}
	return a.ID > b.ID
}

func endedOf(i *domain.Interview) time.Time {
	if i.EndedAt == nil {
		return time.Time{}
	}
	return *i.EndedAt
}
