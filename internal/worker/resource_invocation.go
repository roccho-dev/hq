package worker

// IsVerifiedResourceInvocation reports whether one structurally valid
// local-tool instruction uses the resource-level argv form rather than a
// finite named action. Validation remains authoritative for malformed payloads.
func IsVerifiedResourceInvocation(instruction Instruction) bool {
	return isVerifiedResourceInvocation(instruction)
}
