package a2a

import (
	"testing"
	"time"
)

func TestPublicTaskHonorsHistoryLengthAndArtifactVisibility(t *testing.T) {
	record := TaskRecord{
		ID: "task-1", ContextID: "ctx-1", State: TaskStateCompleted,
		UpdatedAt: nowForTest(),
		History:   []Message{{MessageID: "one"}, {MessageID: "two"}},
		Artifacts: []Artifact{{ArtifactID: "artifact-1", Parts: []Part{TextPart("done")}}},
	}
	view := record.PublicTask(1, false)
	if len(view.History) != 1 || view.History[0].MessageID != "two" {
		t.Fatalf("history = %+v", view.History)
	}
	if view.Artifacts != nil {
		t.Fatalf("artifacts = %+v, want omitted from default view", view.Artifacts)
	}
}

func nowForTest() (value time.Time) { return time.Unix(1, 0).UTC() }
