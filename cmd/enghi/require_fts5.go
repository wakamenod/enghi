//go:build !sqlite_fts5

// This file is compiled only when the sqlite_fts5 build tag is missing, and it
// always fails to build.
//
// Forgetting the tag still builds go-sqlite3 fine, which delays the failure
// until start-up (store.verifyFTS). Search is the most important non-functional
// requirement (DESIGN 3) and a binary without FTS5 is not worth shipping, so we
// put this guard in front of it.
package main

func init() {
	enghi_requires_build_tag_sqlite_fts5__use_make_build()
}
