package main

import (
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestConvertPGValue(t *testing.T) {
	u := uuid.MustParse("0D35514A-7F4E-4E49-9E69-3026028608AD")
	var rawUUID [16]byte
	copy(rawUUID[:], u[:])

	gotUUID, err := convertPGValue(rawUUID, "uuid", "uuid")
	if err != nil || gotUUID != "0d35514a-7f4e-4e49-9e69-3026028608ad" {
		t.Fatalf("uuid conversion = (%v, %v)", gotUUID, err)
	}

	loc := time.FixedZone("UTC+8", 8*3600)
	ts := time.Date(2026, 10, 23, 8, 45, 12, 123456000, loc)
	gotTS, err := convertPGValue(ts, "timestamp with time zone", "timestamptz")
	if err != nil || gotTS != "2026-10-23T00:45:12.123456Z" {
		t.Fatalf("timestamptz conversion = (%v, %v)", gotTS, err)
	}

	gotDate, err := convertPGValue(ts, "date", "date")
	if err != nil || gotDate != "2026-10-23" {
		t.Fatalf("date conversion = (%v, %v)", gotDate, err)
	}

	gotTrue, err := convertPGValue(true, "boolean", "bool")
	if err != nil || gotTrue != int64(1) {
		t.Fatalf("bool true = (%v, %v)", gotTrue, err)
	}
	gotFalse, err := convertPGValue(false, "boolean", "bool")
	if err != nil || gotFalse != int64(0) {
		t.Fatalf("bool false = (%v, %v)", gotFalse, err)
	}

	gotArr, err := convertPGValue([]string{"a", "b"}, "ARRAY", "_text")
	if err != nil || gotArr != `["a","b"]` {
		t.Fatalf("text[] conversion = (%v, %v)", gotArr, err)
	}

	gotUUIDArr, err := convertPGValue(`["0D35514A-7F4E-4E49-9E69-3026028608AD"]`, "ARRAY", "_uuid")
	if err != nil || gotUUIDArr != `["0d35514a-7f4e-4e49-9e69-3026028608ad"]` {
		t.Fatalf("uuid[] conversion = (%v, %v)", gotUUIDArr, err)
	}

	gotXID, err := convertPGValue("1733086", "xid8", "xid8")
	if err != nil || gotXID != int64(1733086) {
		t.Fatalf("xid8 conversion = (%v, %v)", gotXID, err)
	}

	gotInet, err := convertPGValue(netip.MustParsePrefix("192.168.1.10/32"), "inet", "inet")
	if err != nil || gotInet != "192.168.1.10" {
		t.Fatalf("inet conversion = (%v, %v)", gotInet, err)
	}

	gotEmptyBytes, err := convertPGValue([]byte{}, "bytea", "bytea")
	if err != nil {
		t.Fatalf("bytea empty err = %v", err)
	}
	if b, ok := gotEmptyBytes.([]byte); !ok || b == nil || len(b) != 0 {
		t.Fatalf("expected non-nil empty []byte, got %#v", gotEmptyBytes)
	}
}

func TestMapColumnName(t *testing.T) {
	if got := mapColumnName("search_index_change", "change_xid"); got != "change_seq" {
		t.Fatalf("got %q, want change_seq", got)
	}
	if got := mapColumnName("search_index_prune_mark", "pruned_through_xid"); got != "pruned_through_seq" {
		t.Fatalf("got %q, want pruned_through_seq", got)
	}
	if got := mapColumnName("issue", "id"); got != "id" {
		t.Fatalf("got %q, want id", got)
	}
}
