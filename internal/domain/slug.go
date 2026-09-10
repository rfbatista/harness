package domain

import "github.com/rfbatista/harnesskit/slug"

// Slug converts a display name into a filesystem-safe directory name:
// lowercase, with every run of other characters collapsed to a single dash.
// It returns "" when nothing usable remains.
func Slug(name string) string { return slug.Make(name) }
