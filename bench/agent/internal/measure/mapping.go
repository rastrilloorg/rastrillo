package measure

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type SignIn struct {
	URL              string `json:"url"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	UsernameSelector string `json:"username_selector"`
	PasswordSelector string `json:"password_selector"`
	SubmitSelector   string `json:"submit_selector"`
}

// Mapping is where a finished agent says each page role ended up.
type Mapping struct {
	Pages  map[string]string `json:"pages"`
	SignIn SignIn            `json:"signin"`
	// ByRole holds a role's own account where one account cannot open
	// every signed-in page (a member page and an admin page).
	ByRole map[string]SignIn `json:"signin_by_role,omitempty"`
}

func (m Mapping) SignInFor(role string) SignIn {
	if own, given := m.ByRole[role]; given {
		return own
	}
	return m.SignIn
}

// ParseMapping reads the agent's answer. Each role it cannot use is a
// failure naming that role, so one bad URL costs one page, not the run.
func ParseMapping(raw string, roles []string) (Mapping, []string) {
	var mapping Mapping
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start ||
		json.Unmarshal([]byte(raw[start:end+1]), &mapping) != nil {
		return Mapping{Pages: map[string]string{}},
			[]string{"mapping: the answer is not JSON"}
	}
	var failures []string
	kept := map[string]string{}
	for _, role := range roles {
		address, given := mapping.Pages[role]
		if !given || address == "" {
			failures = append(failures, "mapping: no URL for "+role)
			continue
		}
		if problem := liveURLProblem(address); problem != "" {
			failures = append(failures,
				fmt.Sprintf("mapping: %s %s", role, problem))
			continue
		}
		kept[role] = address
	}
	mapping.Pages = kept
	if problem := signInProblem(mapping.SignIn); problem != "" {
		failures = append(failures, "mapping: signin "+problem)
		mapping.SignIn = SignIn{}
	}
	for role, own := range mapping.ByRole {
		if problem := signInProblem(own); problem != "" {
			failures = append(failures,
				fmt.Sprintf("mapping: signin for %s %s", role, problem))
			delete(mapping.ByRole, role)
		}
	}
	return mapping, failures
}

func liveURLProblem(address string) string {
	parsed, err := url.Parse(address)
	switch {
	case err != nil || parsed.Host == "":
		return fmt.Sprintf("is not an absolute URL: %q", address)
	case parsed.Scheme != "https":
		return fmt.Sprintf("is not https: %q", address)
	}
	return ""
}

func signInProblem(details SignIn) string {
	if details.URL == "" {
		return ""
	}
	return liveURLProblem(details.URL)
}
