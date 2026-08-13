package buildinfo

import (
	"strings"
	"testing"
)

func TestFormatUsesSharedPublicContract(t *testing.T) {
	output := Format("agent-workflow-test", 1)
	for _, field := range []string{"agent-workflow-test version=0.2.0", "protocol=1", "maturity=experimental", "revision=", "dirty="} {
		if !strings.Contains(output, field) {
			t.Fatalf("version output %q missing %q", output, field)
		}
	}
}
