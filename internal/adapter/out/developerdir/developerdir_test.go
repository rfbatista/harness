package developerdir

import (
	"context"
	"errors"
	"testing"
)

func TestBinIsTheActiveDeveloperDirsUsrBin(t *testing.T) {
	got, err := bin("darwin", func(context.Context) ([]byte, error) {
		return []byte("/Applications/Xcode.app/Contents/Developer\n"), nil
	})
	if err != nil || got != "/Applications/Xcode.app/Contents/Developer/usr/bin" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestThereIsNoBinOffMacOSOrWhenXcodeSelectFails(t *testing.T) {
	never := func(context.Context) ([]byte, error) { t.Fatal("xcode-select ran off macOS"); return nil, nil }
	if _, err := bin("linux", never); err == nil {
		t.Error("a bin off macOS")
	}
	if _, err := bin("darwin", func(context.Context) ([]byte, error) { return nil, errors.New("no developer dir") }); err == nil {
		t.Error("a bin when xcode-select fails")
	}
	if _, err := bin("darwin", func(context.Context) ([]byte, error) { return []byte("\n"), nil }); err == nil {
		t.Error("a bin from an empty answer")
	}
}
