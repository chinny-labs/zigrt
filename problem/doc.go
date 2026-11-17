// SPDX-License-Identifier: Apache-2.0

// Package problem turns declarative problem templates into problems and
// checks answers to them (ADR 9). A problem is a template plus a seed: the
// same seed always gives the same problem, so any attempt can be replayed and
// any worksheet reprinted.
//
// The package is pure. It reads no clock and no global random source, and it
// never runs code from a pack: templates are data, and their expressions are
// evaluated by package expr with a step budget.
package problem
