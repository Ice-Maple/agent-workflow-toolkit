package artifact

import (
	"path/filepath"
	"testing"
)

func BenchmarkStageAtomicReplace(b *testing.B) {
	workspace := b.TempDir()
	path := filepath.Join(workspace, "reports", "review.md")
	content := "# Review\n\n## Findings\n\nPASS\n"
	requiredSections := []string{"# Review", "## Findings"}
	b.ReportMetric(float64(len(content)), "content_bytes/op")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Stage(workspace, path, "markdown", content, requiredSections); err != nil {
			b.Fatal(err)
		}
	}
}
