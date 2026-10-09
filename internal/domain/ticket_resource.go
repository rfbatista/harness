package domain

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A person gives a task context of their own: resources (uploaded files,
// images, audio, links) and comments. See "Contract: Harness server ↔ Web UI
// — task resources and comments". They are not documents (pages sessions
// write) nor attached project assets (artifacts).

// ResourceKind is what a resource is, derived from its MIME type for an
// upload; a link is created as a link.
type ResourceKind string

const (
	ResourceFile  ResourceKind = "file"
	ResourceImage ResourceKind = "image"
	ResourceAudio ResourceKind = "audio"
	ResourceLink  ResourceKind = "link"
)

const (
	// MaxResourceBytes caps one uploaded file.
	MaxResourceBytes int64 = 25 << 20
	// MaxResourceTitleLen caps a resource title, in characters.
	MaxResourceTitleLen = 200
	// MaxResourceURLLen caps a link's URL, in characters.
	MaxResourceURLLen = 2048
	// MaxCommentLen caps a comment body, in characters.
	MaxCommentLen = 10000
)

// TicketResource is something a person added to one task for context.
type TicketResource struct {
	ID        string       `json:"id"`
	TicketID  string       `json:"ticket_id"`
	ProjectID string       `json:"project_id"`
	Kind      ResourceKind `json:"kind"`
	Title     string       `json:"title"`
	Mime      string       `json:"mime"`       // "" for a link
	SizeBytes int64        `json:"size_bytes"` // 0 for a link
	URL       string       `json:"url"`        // the link target; "" for an upload
	// ContentURL is where the bytes are served; "" for a link. The HTTP
	// adapter owns the route, so the service fills it from a function it is
	// given.
	ContentURL string    `json:"content_url"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IsUpload says whether the resource has bytes (anything but a link).
func (r *TicketResource) IsUpload() bool { return r.Kind != ResourceLink }

// CommentAuthor says who wrote a comment. Only a person does, for now.
type CommentAuthor string

const CommentByPerson CommentAuthor = "person"

// TicketComment is a plain-text note on a task, in one chronological thread
// per task.
type TicketComment struct {
	ID        string        `json:"id"`
	TicketID  string        `json:"ticket_id"`
	ProjectID string        `json:"project_id"`
	Author    CommentAuthor `json:"author"`
	Body      string        `json:"body"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"` // later than CreatedAt once edited
}

// MediaType is mime's lower-cased type/subtype, without parameters.
func MediaType(mime string) string {
	base, _, _ := strings.Cut(mime, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// ResourceKindFor is the kind of an upload of the given MIME type.
func ResourceKindFor(mime string) ResourceKind {
	switch t := MediaType(mime); {
	case strings.HasPrefix(t, "image/"):
		return ResourceImage
	case strings.HasPrefix(t, "audio/"):
		return ResourceAudio
	}
	return ResourceFile
}

// ResourceTitle is title trimmed, when it is 1–MaxResourceTitleLen characters.
func ResourceTitle(title string) (string, error) {
	t := strings.TrimSpace(title)
	if t == "" {
		return "", invalidInput("title is required")
	}
	if utf8.RuneCountInString(t) > MaxResourceTitleLen {
		return "", invalidInput("title is limited to " + strconv.Itoa(MaxResourceTitleLen) + " characters")
	}
	return t, nil
}

// DefaultResourceTitle is the title an upload gets when the person gives
// none: the file name's base, whichever separator the browser kept, cut to
// the title limit.
func DefaultResourceTitle(fileName string) string {
	name := fileName
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	return cutRunes(strings.TrimSpace(name), MaxResourceTitleLen)
}

// DefaultLinkTitle is the title a link gets when the person gives none: the
// URL itself, cut to the title limit.
func DefaultLinkTitle(url string) string { return cutRunes(url, MaxResourceTitleLen) }

// LinkURL is raw trimmed, when it is an http or https URL with a host of at
// most MaxResourceURLLen characters.
func LinkURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", invalidInput("url is required")
	}
	if utf8.RuneCountInString(s) > MaxResourceURLLen {
		return "", invalidInput("url is limited to " + strconv.Itoa(MaxResourceURLLen) + " characters")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", invalidInput("url must be an http or https address")
	}
	return s, nil
}

// CommentBody is body trimmed, when it is 1–MaxCommentLen characters. Line
// breaks inside it are kept.
func CommentBody(body string) (string, error) {
	b := strings.TrimSpace(body)
	if b == "" {
		return "", invalidInput("body is required")
	}
	if utf8.RuneCountInString(b) > MaxCommentLen {
		return "", invalidInput("body is limited to " + strconv.Itoa(MaxCommentLen) + " characters")
	}
	return b, nil
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
