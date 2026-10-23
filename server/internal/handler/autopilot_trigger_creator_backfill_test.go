package handler

import (

)

// Migration 467 fills a legacy trigger's missing creator from its autopilot's
// creator, so schedule/webhook dispatch has a principal again instead of
// skipping every run (#8284).
//
// The shipped SQL runs twice inside a transaction that is rolled back: the
// UPDATE is table-wide, and committing it would rewrite the NULL-creator
// triggers that other packages' tests build concurrently in the same database.

func orNULL(s *string) string {
	if s == nil {
		return "NULL"
	}
	return *s
}
