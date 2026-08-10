package artifact

import (
	"path/filepath"
	"strings"
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

func BenchmarkStageLargeAtomicReplace(b *testing.B) {
	workspace := b.TempDir()
	path := filepath.Join(workspace, "reports", "large-review.md")
	content := "# Review\n\n## Findings\n\n" + strings.Repeat("finding\n", 32*1024)
	requiredSections := []string{"# Review", "## Findings"}
	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Stage(workspace, path, "markdown", content, requiredSections); err != nil {
			b.Fatal(err)
		}
	}
}
