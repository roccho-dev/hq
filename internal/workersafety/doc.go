// Package workersafety defines the project-local artifact layout, durable-log
// redaction, and fail-closed approval decisions used by a JSONL worker.
//
// It is intentionally independent from hq compiler code and from concrete
// execution adapters. The worker may compose these pure contracts before any
// adapter side effect begins.
package workersafety
