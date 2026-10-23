// Package security owns the Milestone 1 security contracts shared by domain
// services, HTTP/UI handlers, and PostgreSQL adapters.
//
// This package intentionally depends only on the standard library. It defines
// ports, policy, and data types; it does not implement login, session persistence,
// middleware, or database adapters. See docs/security-contracts.md before
// implementing a port: atomicity and failure semantics are part of the contract.
package security
