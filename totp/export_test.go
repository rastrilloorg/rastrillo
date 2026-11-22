package totp

// SetBeforeClaim lets a test run hook inside PendingOrBegin, after its
// secret is made and before it is written; nil removes it.
func SetBeforeClaim(hook func()) { beforeClaim = hook }
