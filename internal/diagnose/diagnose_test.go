package diagnose

import (
	"strings"
	"testing"

	"github.com/routerr/aubar/internal/domain"
)

func TestCollectionBasicError(t *testing.T) {
	col := domain.Collection{
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider: domain.ProviderOpenAI,
				Status:   domain.StatusError,
				Source:   "cli",
				Reason:   "command not found",
			},
		},
	}

	lines := Collection(col)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "CLI execution failed") {
		t.Fatalf("expected CLI failure guidance, got %q", joined)
	}
}
