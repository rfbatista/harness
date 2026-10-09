package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"
)

func TestResourceKindFor(t *testing.T) {
	cases := map[string]ResourceKind{
		"image/png":                 ResourceImage,
		"IMAGE/JPEG":                ResourceImage,
		"image/svg+xml":             ResourceImage,
		"audio/mpeg":                ResourceAudio,
		"audio/webm; codecs=opus":   ResourceAudio,
		"application/pdf":           ResourceFile,
		"text/plain; charset=utf-8": ResourceFile,
		"":                          ResourceFile,
		"not a mime":                ResourceFile,
	}
	for mime, want := range cases {
		if got := ResourceKindFor(mime); got != want {
			t.Errorf("ResourceKindFor(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestMediaType(t *testing.T) {
	cases := map[string]string{
		"Text/Plain; charset=UTF-8": "text/plain",
		"application/json":          "application/json",
		"":                          "",
		" image/png ":               "image/png",
	}
	for in, want := range cases {
		if got := MediaType(in); got != want {
			t.Errorf("MediaType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResourceTitle(t *testing.T) {
	got, err := ResourceTitle("  Spec  ")
	if err != nil || got != "Spec" {
		t.Fatalf("ResourceTitle = %q, %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("é", MaxResourceTitleLen+1)} {
		if _, err := ResourceTitle(bad); errs.Code(err) != "INVALID_INPUT" {
			t.Errorf("ResourceTitle(%d runes) err = %v, want INVALID_INPUT", len([]rune(bad)), err)
		}
	}
	if _, err := ResourceTitle(strings.Repeat("é", MaxResourceTitleLen)); err != nil {
		t.Errorf("a title of exactly the limit is refused: %v", err)
	}
}

func TestDefaultResourceTitleCutsLongNames(t *testing.T) {
	long := strings.Repeat("a", MaxResourceTitleLen+50) + ".txt"
	if got := DefaultResourceTitle(long); len([]rune(got)) != MaxResourceTitleLen {
		t.Fatalf("DefaultResourceTitle kept %d runes", len([]rune(got)))
	}
	if got := DefaultResourceTitle(`C:\Users\me\notes.txt`); got != "notes.txt" {
		t.Fatalf("windows path = %q", got)
	}
	if got := DefaultResourceTitle("dir/sub/photo.png"); got != "photo.png" {
		t.Fatalf("unix path = %q", got)
	}
}

func TestLinkURL(t *testing.T) {
	for _, ok := range []string{"https://example.com", "http://example.com/a?b=c#d", "  https://x.io/  "} {
		if _, err := LinkURL(ok); err != nil {
			t.Errorf("LinkURL(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "ftp://example.com", "javascript:alert(1)", "https://", "example.com", "https://" + strings.Repeat("a", MaxResourceURLLen)} {
		if _, err := LinkURL(bad); errs.Code(err) != "INVALID_INPUT" {
			t.Errorf("LinkURL(%.40q) err = %v, want INVALID_INPUT", bad, err)
		}
	}
	if got, _ := LinkURL("  https://x.io/  "); got != "https://x.io/" {
		t.Errorf("LinkURL does not trim: %q", got)
	}
}

func TestCommentBody(t *testing.T) {
	got, err := CommentBody("\n  first line\nsecond line  \n")
	if err != nil || got != "first line\nsecond line" {
		t.Fatalf("CommentBody = %q, %v", got, err)
	}
	if _, err := CommentBody(strings.Repeat("é", MaxCommentLen)); err != nil {
		t.Errorf("a body of exactly the limit is refused: %v", err)
	}
	for _, bad := range []string{"", " \n\t ", strings.Repeat("é", MaxCommentLen+1)} {
		if _, err := CommentBody(bad); errs.Code(err) != "INVALID_INPUT" {
			t.Errorf("CommentBody(%d runes) err = %v, want INVALID_INPUT", len([]rune(bad)), err)
		}
	}
}

func TestCommentWrittenBy(t *testing.T) {
	mine := &TicketComment{Author: CommentBySession, SessionID: "s1"}
	if !mine.WrittenBy("s1") {
		t.Error("a session's own comment is written by it")
	}
	if mine.WrittenBy("s2") {
		t.Error("another session's comment is not written by the caller")
	}
	person := &TicketComment{Author: CommentByPerson}
	if person.WrittenBy("") || person.WrittenBy("s1") {
		t.Error("a person's comment is written by no session")
	}
}

func TestCommentWireShapeAlwaysCarriesTheAuthorFields(t *testing.T) {
	b, err := json.Marshal(&TicketComment{ID: "c", Author: CommentByPerson, Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"author":"person"`, `"session_id":""`, `"agent_name":""`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("%s lacks %s", b, key)
		}
	}
}
