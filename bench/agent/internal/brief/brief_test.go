package brief

import (
	"strings"
	"testing"
)

const benchRoot = "../.."

func TestLoadAllReadsEveryBrief(t *testing.T) {
	briefs, err := LoadAll(benchRoot)
	if err != nil {
		t.Fatal(err)
	}
	var names, sets []string
	for _, b := range briefs {
		names = append(names, b.Name)
		sets = append(sets, b.Set)
	}
	if got := strings.Join(names, ","); got != "groomer,lending,noticeboard,smoke" {
		t.Errorf("briefs = %s", got)
	}
	if got := strings.Join(sets, ","); got != "dev,heldout,heldout,smoke" {
		t.Errorf("sets = %s", got)
	}
	groomer := briefs[0]
	if len(groomer.Pages) != 4 {
		t.Fatalf("groomer pages = %+v, want 4", groomer.Pages)
	}
	owner := groomer.Pages[3]
	if owner.Role != "owner" || !owner.SignedIn || owner.Description == "" {
		t.Errorf("owner page = %+v, want a signed-in role with a description",
			owner)
	}
	if groomer.Pages[0].SignedIn {
		t.Error("home is marked signed-in")
	}
	if len(groomer.Words) == 0 || groomer.Followup == "" {
		t.Errorf("groomer lost its words or follow-up: %+v", groomer)
	}
}

func TestPromptFillsVarsAndKeepsArmsApart(t *testing.T) {
	groomer, err := Load(benchRoot + "/briefs/groomer")
	if err != nil {
		t.Fatal(err)
	}
	vars := Vars{
		App: "groomer-r1-1", Platform: "Fly.io",
		SkillURL: "http://100.1.2.3:8080/SKILL.md",
	}

	carlos, err := groomer.Prompt(Carlos, vars)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{vars.SkillURL, vars.App, "dog-grooming"} {
		if !strings.Contains(carlos, want) {
			t.Errorf("carlos prompt lacks %q", want)
		}
	}
	control, err := groomer.Prompt(Control, vars)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(control, "Fly.io") {
		t.Error("control prompt lacks the platform")
	}
	for _, prompt := range []string{carlos, control} {
		if strings.Contains(prompt, "{{") || strings.Contains(prompt, "<no value>") {
			t.Errorf("unfilled template in:\n%s", prompt)
		}
	}
	lower := strings.ToLower(control)
	for _, framework := range []string{"rastrillo", "carlos"} {
		if strings.Contains(lower, framework) {
			t.Errorf("control prompt mentions %q", framework)
		}
	}
}

func TestMappingListsRolesAndAsksForSignInOnlyWhenNeeded(t *testing.T) {
	briefs, err := LoadAll(benchRoot)
	if err != nil {
		t.Fatal(err)
	}
	groomer, smoke := briefs[0], briefs[3]

	asked, err := Mapping(benchRoot, groomer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "- owner: the owner's list of bookings (needs sign-in)") {
		t.Errorf("groomer mapping prompt:\n%s", asked)
	}
	if !strings.Contains(asked, "CSS selectors") {
		t.Error("groomer mapping does not ask for sign-in details")
	}
	asked, err = Mapping(benchRoot, smoke)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(asked, "CSS selectors") {
		t.Error("smoke mapping asks for sign-in details it cannot have")
	}
}

func TestEnvNamesTheDeployAccount(t *testing.T) {
	env, err := Env(benchRoot, Vars{DeployAccount: `the Carloku account "demo"`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env, `Deploy only to the Carloku account "demo".`) {
		t.Errorf("env:\n%s", env)
	}
}

func TestSpecArmMeetsTheSpecWithoutTheFramework(t *testing.T) {
	groomer, err := Load(benchRoot + "/briefs/groomer")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := groomer.Prompt(Spec, Vars{App: "groomer-r2-1", Platform: "Fly.io"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://spec.carlosframework.com", "Fly.io", "groomer-r2-1"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("spec prompt lacks %q:\n%s", want, prompt)
		}
	}
	// The spec names no framework; the prompt must not either, or the
	// arm stops measuring the bar without the framework.
	for _, framework := range []string{"rastrillo", "carloku", "rastrillo.org"} {
		if strings.Contains(strings.ToLower(prompt), framework) {
			t.Errorf("spec prompt mentions %q", framework)
		}
	}
}
