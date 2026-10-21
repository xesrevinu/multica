package taskfailure

import "testing"

func TestClassifyYieldedPersistenceBounds(t *testing.T) {
	if got := Classify("Retained model context exceeds persistence bounds after elision"); got != ReasonAgentContextOverflow {
		t.Fatalf("got %s", got)
	}
}
