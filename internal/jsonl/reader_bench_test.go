package jsonl

import (
	"strings"
	"testing"
)

// This file replaces the previous -tags zig benchmark suite. It exercises the
// pure-Go streaming path on a representative mix of record sizes so that any
// future regression in the rolling-buffer / bytes.IndexByte loop is visible
// in `go test -bench`.

func makeInput(lines, lineLen int) string {
	line := strings.Repeat("x", lineLen-1) + "\n"
	return strings.Repeat(line, lines)
}

func benchmarkForEachLine(b *testing.B, fn func(r *strings.Reader) error, input string) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fn(strings.NewReader(input))
	}
}

func BenchmarkSmallLines(b *testing.B) {
	input := makeInput(100000, 20)
	benchmarkForEachLine(b, func(r *strings.Reader) error {
		var sum int
		return ForEachLine(r, func(line []byte) error {
			sum += len(line)
			return nil
		})
	}, input)
}

func BenchmarkMediumLines(b *testing.B) {
	input := makeInput(10000, 200)
	benchmarkForEachLine(b, func(r *strings.Reader) error {
		var sum int
		return ForEachLine(r, func(line []byte) error {
			sum += len(line)
			return nil
		})
	}, input)
}

func BenchmarkLargeLines(b *testing.B) {
	input := makeInput(10000, 1000)
	benchmarkForEachLine(b, func(r *strings.Reader) error {
		var sum int
		return ForEachLine(r, func(line []byte) error {
			sum += len(line)
			return nil
		})
	}, input)
}

func BenchmarkHugeLine(b *testing.B) {
	input := strings.Repeat("x", 3*1024*1024) + "\n"
	benchmarkForEachLine(b, func(r *strings.Reader) error {
		var sum int
		return ForEachLine(r, func(line []byte) error {
			sum += len(line)
			return nil
		})
	}, input)
}

func BenchmarkMixed(b *testing.B) {
	// 10000 short lines + 1 medium line + 5000 long lines.
	var sb strings.Builder
	short := strings.Repeat("s", 19) + "\n"
	med := strings.Repeat("m", 999) + "\n"
	long := strings.Repeat("l", 4095) + "\n"
	for i := 0; i < 10000; i++ {
		sb.WriteString(short)
	}
	sb.WriteString(med)
	for i := 0; i < 5000; i++ {
		sb.WriteString(long)
	}
	input := sb.String()
	benchmarkForEachLine(b, func(r *strings.Reader) error {
		var sum int
		return ForEachLine(r, func(line []byte) error {
			sum += len(line)
			return nil
		})
	}, input)
}

func BenchmarkNoTrailingNewline(b *testing.B) {
	input := strings.Repeat("x", 1024) + "\n" + strings.Repeat("y", 1024) + "\nlast"
	benchmarkForEachLine(b, func(r *strings.Reader) error {
		var sum int
		return ForEachLine(r, func(line []byte) error {
			sum += len(line)
			return nil
		})
	}, input)
}
