// Package utils provides string utility functions.
//
// Author: Claude
package utils

import (
	"strings"

	"github.com/rivo/uniseg"
)

// WrapEveryNRunes inserts a newline every n runes.
func WrapEveryNRunes(s string, n int) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + len(runes)/n)
	for i, r := range runes {
		if i > 0 && i%n == 0 {
			b.WriteByte('\n')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// WrapAtWidth wraps s so that no line exceeds maxWidth display columns,
// breaking only at Unicode line-segment boundaries and never mid-word.
// A trailing newline is preserved only if s itself ends with one.
func WrapAtWidth(s string, maxWidth int) string {
	var buf strings.Builder
	buf.Grow(len(s) + len(s)/maxWidth)

	state := -1
	lineWidth := 0
	lineStart := true // track whether we are at the start of a new line

	for remaining := s; len(remaining) > 0; {
		seg, rest, mustBreak, newState := uniseg.FirstLineSegmentInString(remaining, state)
		remaining, state = rest, newState

		segWidth := uniseg.StringWidth(seg)

		// If the segment alone exceeds maxWidth, emit it as-is rather than looping forever.
		if lineStart && segWidth > maxWidth {
			buf.WriteString(seg)
			lineWidth = segWidth
			lineStart = false
		} else if !lineStart && lineWidth+segWidth > maxWidth {
			// Strip leading spaces from the wrapped segment to avoid
			// a dangling indent at the start of the new line.
			trimmed := strings.TrimLeft(seg, " ")
			trimWidth := uniseg.StringWidth(trimmed)

			buf.WriteByte('\n')
			buf.WriteString(trimmed)
			lineWidth = trimWidth
			lineStart = false
		} else {
			buf.WriteString(seg)
			lineWidth += segWidth
			lineStart = false
		}

		if mustBreak {
			lineWidth = 0
			lineStart = true
		}
	}

	return buf.String()
}
