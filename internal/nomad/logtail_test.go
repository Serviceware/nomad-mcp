package nomad

import (
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func TestTrimLogTailDropsLeadingPartialLineWhenClipped(t *testing.T) {
	t.Parallel()

	// A byte-offset read from the end of the file lands mid-line.
	clipped := "ne that was cut in half\nsecond\nthird\n"
	must.Eq(t, "second\nthird\n", trimLogTail(clipped, 10, true))

	// Without clipping the first line is complete and must be kept.
	must.Eq(t, clipped, trimLogTail(clipped, 10, false))
}

func TestTrimLogTailKeepsSingleOversizedLine(t *testing.T) {
	t.Parallel()

	// One line longer than the whole byte budget: dropping it would return nothing,
	// so the partial line is kept.
	must.Eq(t, "no newline anywhere", trimLogTail("no newline anywhere", 10, true))
}

func TestTrimLogTailBoundsLineCount(t *testing.T) {
	t.Parallel()

	must.Eq(t, "d\ne\n", trimLogTail("a\nb\nc\nd\ne\n", 2, false))
	must.Eq(t, "d\ne", trimLogTail("a\nb\nc\nd\ne", 2, false))

	// Clipping drops the partial "a" first, leaving four lines to trim to three.
	must.Eq(t, "c\nd\ne\n", trimLogTail("a\nb\nc\nd\ne\n", 3, true))
}

func TestLastLinesPassesThroughShortInput(t *testing.T) {
	t.Parallel()

	must.Eq(t, "only\n", lastLines("only\n", 5))
	must.Eq(t, "", lastLines("", 5))
	must.Eq(t, "a\nb\n", lastLines("a\nb\n", 2))
}

func TestTrimLogTailRespectsRequestedLinesOnLargeInput(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	for i := 0; i < 1000; i++ {
		builder.WriteString("line\n")
	}

	trimmed := trimLogTail(builder.String(), 200, true)
	must.Eq(t, 200, strings.Count(trimmed, "\n"))
}
