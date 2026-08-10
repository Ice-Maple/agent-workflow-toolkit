package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func BenchmarkParseJSONLReplay(b *testing.B) {
	raw := readWave4ReplayFixture(b, "cursor-success.jsonl")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		summary := ParseJSONL(raw)
		if !summary.DeliveryComplete() {
			b.Fatal("replay fixture did not produce complete delivery")
		}
	}
}

func BenchmarkStreamSummaryAddLine(b *testing.B) {
	raw := readWave4ReplayFixture(b, "cursor-success.jsonl")
	lines := bytes.Split(raw, []byte{'\n'})
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var summary StreamSummary
		for _, line := range lines {
			if err := summary.AddLine(line); err != nil {
				b.Fatal(err)
			}
		}
		if !summary.DeliveryComplete() {
			b.Fatal("fixture did not produce complete delivery")
		}
	}
}

func readWave4ReplayFixture(b *testing.B, name string) []byte {
	b.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "tests", "orchestration", "fixtures", "wave4", "replay", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read replay fixture: %v", err)
	}
	return raw
}
