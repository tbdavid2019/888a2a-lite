package service

import (
	"testing"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

func TestNextCircleKeyVersionUsesHighestPersistedVersion(t *testing.T) {
	keys := []hub.CircleKey{
		{Version: 1},
		{Version: 4},
		{Version: 2},
	}
	if got := nextCircleKeyVersion(2, keys); got != 5 {
		t.Fatalf("next circle key version = %d, want 5", got)
	}
}

func TestNextCircleKeyVersionStartsAfterActiveVersion(t *testing.T) {
	if got := nextCircleKeyVersion(0, nil); got != 1 {
		t.Fatalf("next circle key version = %d, want 1", got)
	}
}
