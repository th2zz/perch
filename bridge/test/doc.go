// Package test holds cross-package contract tests.
//
// These tests exist because the constitution makes three claims that are
// checked mechanically rather than by inspection:
//
//	II  every write the product performs equals one herdr command;
//	IV  there are exactly four kinds of write and no fifth;
//	V.8 a push payload carries a summary and nothing else.
//
// They live outside the packages they check on purpose: a test inside
// internal/actions can only see that package's own view of the mapping, whereas
// the claim being made is about the wire contract in contracts/herdr-usage.md.
package test
