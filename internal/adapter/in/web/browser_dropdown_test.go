package web

import (
	"context"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// rawKey presses a named key (Enter, ArrowDown, Escape…) as one raw keydown
// and a keyup, with no separate keypress: a dropdown that closes on keydown
// would otherwise see the keypress land on whatever took the focus.
func rawKey(key string, code int64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		down := input.DispatchKeyEvent(input.KeyRawDown).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code)
		if err := down.Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code).Do(ctx)
	})
}

func pressEnter() chromedp.Action     { return rawKey("Enter", 13) }
func pressArrowDown() chromedp.Action { return rawKey("ArrowDown", 40) }
