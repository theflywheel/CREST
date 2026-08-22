// Package csv is the reference source-system adapter.
//
// CSV comes first deliberately: it unblocks every source system immediately,
// whatever their integration maturity (#22). It is also the reference
// implementation of the adapter contract (Blueprint §8) — the claim that a new
// source class connects through configuration rather than code (#30) is tested
// against what this package establishes.
//
// Implementation arrives with #22.
package csv
