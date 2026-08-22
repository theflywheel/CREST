// Package harness holds the CREST end-to-end test harness.
//
// One command, real services, no human in the loop (#40). Contract in
// docs/TESTING.md; the rules that matter most are: poll health rather than
// sleep, drive the system through its real interfaces, advance the injected
// clock instead of waiting, and be deterministic or be deleted.
package harness
