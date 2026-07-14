// Package sidethread owns the side-thread model used by /side. A side
// thread is a per-main-thread companion conversation the renderer
// opens through `/side`; it mirrors the product design in
// docs/plans/2026-07-13-side-thread-product-design.md.
//
// Side threads are stored separately from regular sessions, one JSON
// file per main thread under <SessionDir>/sidethreads/<main_thread_id>.json.
// They never appear in the global session list, and each main thread
// owns at most one side thread.
package sidethread
