package dbid

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestNewV7ReturnsAValidVersion7UUID(t *testing.T) {
	got := NewV7()
	if got == "" {
		t.Fatal("NewV7 returned an empty id")
	}
	parsed, err := uuid.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if v := parsed.Version(); v != 7 {
		t.Fatalf("uuid version = %d, want 7 (%s)", v, parsed)
	}
	if vr := parsed.Variant(); vr != uuid.RFC4122 {
		t.Fatalf("uuid variant = %v, want RFC4122 (%s)", vr, parsed)
	}
}

func TestNewV7IsUniqueAndNonDecreasing(t *testing.T) {
	const n = 500

	seen := make(map[uuid.UUID]struct{}, n)
	var prev []byte
	for i := 0; i < n; i++ {
		raw := NewV7()
		if raw == "" {
			t.Fatalf("call %d returned an empty uuid", i)
		}
		parsed, err := uuid.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[parsed]; dup {
			t.Fatalf("call %d produced a duplicate id %s", i, parsed)
		}
		seen[parsed] = struct{}{}

		// The first 6 bytes are the big-endian millisecond timestamp. Within one
		// process they must never go backwards, which is the whole point of using
		// v7 for a primary key: byte order tracks insert order, so consecutive
		// rows land on the same B-tree page.
		ts := parsed[:6]
		if prev != nil && bytes.Compare(ts, prev) < 0 {
			t.Fatalf("call %d timestamp prefix went backwards: %x < %x", i, ts, prev)
		}
		prev = append([]byte(nil), ts...)
	}
}
