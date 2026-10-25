package carlos

import "os"

// HostHeader is the response header an instance uses to say which
// platform host answered. X-Carlos-Version names a release, and two
// apps' releases can share an identifier (a scaffold ships hub and
// tenant from one commit), so a hub proving that its path reaches the
// tenant cannot tell the tenant from itself by the release alone.
const HostHeader = "Rastrillo-Host"

// hostEnv is the instance's own platform hostname, set by the platform.
const hostEnv = "CARLOS_HOST"

// Host returns $CARLOS_HOST, the platform hostname this instance
// answers on, or "" off CARLOS. It is read on each call, not cached, so
// a test can set it per case.
func Host() string {
	return os.Getenv(hostEnv)
}
