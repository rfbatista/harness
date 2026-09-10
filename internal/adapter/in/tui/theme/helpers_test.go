package theme

import (
	"fmt"
	"image/color"
	"strings"
)

func colorKey(c color.Color) string {
	r, g, b, a := c.RGBA()
	return fmt.Sprintf("%d-%d-%d-%d", r, g, b, a)
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
