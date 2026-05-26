package gitlab

import (
	"strings"
	"testing"
)

func TestChownPathRecursive_rejectsOutsideFluidTmp(t *testing.T) {
	err := ChownPathRecursive("/etc/passwd", "nobody")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "/tmp/fluid") {
		t.Fatalf("unexpected error: %v", err)
	}
}
