//go:build browser

package harness

// WithoutPRFAtCreation rehearses the browsers that refuse to return
// PRF output during creation — webauthn.mjs's two-prompt fallback,
// where register() runs an immediate assertion to fetch it. The CDP
// virtual authenticator cannot withhold PRF at creation (HasPrf is
// all-or-nothing, SetResponseOverrideBits included), so the condition
// is forced one level up with a page-level shim, registered in the
// main world before any navigation.
//
// The mechanism matters; two tempting shapes fail. Patching
// PublicKeyCredential.prototype.getClientExtensionResults strips PRF
// from assertions too — the fallback's own
// assertion.getClientExtensionResults() goes through the same
// prototype method, so the patch would break the very path under
// test. A naive Proxy around the credential throws "Illegal
// invocation" on the brand-checked response/rawId getters. What works,
// and what register()'s access pattern permits (it reads only rawId,
// response.*, and calls getClientExtensionResults() on the instance):
// wrap navigator.credentials.create and define an OWN property on the
// returned credential that answers {} — creation succeeds, the
// extension result is empty, and credentials.get stays untouched, so
// the virtual authenticator serves real PRF on the fallback assertion.
func WithoutPRFAtCreation() Option {
	return func(c *config) { c.withoutPRFAtCreation = true }
}

// prfShimJS is the shim WithoutPRFAtCreation registers via
// Page.addScriptToEvaluateOnNewDocument — main world (no isolated
// world name), so the page's own module sees the wrapped create.
const prfShimJS = `(() => {
  const create = navigator.credentials.create.bind(navigator.credentials);
  navigator.credentials.create = async (options) => {
    const credential = await create(options);
    if (credential) {
      Object.defineProperty(credential, "getClientExtensionResults", {
        value: () => ({}),
        configurable: true,
      });
    }
    return credential;
  };
})();`
