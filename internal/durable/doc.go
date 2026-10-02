// Package durable implements local transaction outcomes, original command
// admission, revision-aware leased jobs and bounded work. Trusted assembly owns
// Engine; domain repositories receive Tx and handlers receive scope-bound Work.
// Domain authorization, effects, success and safe retry remain domain ports.
package durable
