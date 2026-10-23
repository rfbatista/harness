package seatbelt

import (
	"testing"
	"time"
)

func TestParseViolationReadsOperationPathAndProcess(t *testing.T) {
	line := `{"eventMessage":"Sandbox: cat(50090) deny(1) file-read-data \/Users\/me\/my dir\/.ssh\/id_rsa\nharness-sandbox:\/wt","timestamp":"2026-10-23 07:17:56.580978-0300"}`
	v, tag, ok := parseViolation([]byte(line))
	if !ok {
		t.Fatal("not parsed")
	}
	if tag != "harness-sandbox:/wt" || v.Process != "cat" || v.Operation != "file-read-data" || v.Path != "/Users/me/my dir/.ssh/id_rsa" {
		t.Fatalf("got %+v tag %q", v, tag)
	}
	want := time.Date(2026, 10, 23, 7, 17, 56, 580978000, time.FixedZone("", -3*3600))
	if !v.When.Equal(want) {
		t.Fatalf("when %v, want %v", v.When, want)
	}
}

func TestParseViolationKeepsOperationsWithoutAPathAndProcessNamesWithParens(t *testing.T) {
	v, tag, ok := parseViolation([]byte(`{"eventMessage":"Sandbox: Google Chrome (x)(77) deny(1) network-outbound","timestamp":"2026-10-23 07:17:56.580978-0300"}`))
	if !ok || tag != "" || v.Process != "Google Chrome (x)" || v.Operation != "network-outbound" || v.Path != "" {
		t.Fatalf("got %+v %q %v", v, tag, ok)
	}
}

func TestParseViolationIgnoresWhatIsNotADenial(t *testing.T) {
	for _, l := range []string{
		`Filtering the log data using "sender == ..."`,
		`{"eventMessage":"Sandbox: cat(1) allow file-read-data /x"}`,
		`{"eventMessage":"something else deny"}`,
		``,
	} {
		if _, _, ok := parseViolation([]byte(l)); ok {
			t.Errorf("parsed %q", l)
		}
	}
}
