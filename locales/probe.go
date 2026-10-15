package locales

import (
	"encoding/json"
	"fmt"
	"io"
)

// ProbeReport is the probe's output: one JSON object, these fields
// always present, so doctor and scripts can rely on them. Version is the
// report's own format version.
type ProbeReport struct {
	Version       int               `json:"rastrillo_check_config"`
	OK            bool              `json:"ok"`
	Default       string            `json:"default"`
	DefaultFormat string            `json:"default_format"`
	Offered       []string          `json:"offered"`
	Formats       map[string]string `json:"formats"`
	PacksRequired []string          `json:"packs_required"`
	PacksLinked   []string          `json:"packs_linked"`
	Problems      []string          `json:"problems"`
}

// Probe is the configuration probe's logic, which the app's entry
// points run before anything else: before the database is opened,
// before migrations, before background or sidecar work. env is
// RASTRILLO_CHECK_CONFIG.
//
// It returns run=false when the variable is unset, and the process
// carries on. Otherwise the process must exit with the returned code
// and never serve: 0 for an accepted configuration, 1 for problems,
// 2 for a value other than "1". It never warns and serves, because a
// probe that could serve would let doctor start a real server against a
// live database, and an inherited variable in production is a
// misconfiguration that should stop the instance where its supervisor
// and logs show it, not leave it running.
func Probe(env, defaultLocale string, offered []string, stdout, stderr io.Writer) (run bool, exit int) {
	if env == "" {
		return false, 0
	}
	if env != "1" {
		fmt.Fprintf(stderr, "RASTRILLO_CHECK_CONFIG=%q: set it to 1 to check the configuration, or unset it to serve\n", env)
		return true, 2
	}
	cfg, err := CheckConfig(defaultLocale, offered)
	r := ProbeReport{
		Version: 1, OK: err == nil,
		Default: cfg.Default, DefaultFormat: cfg.DefaultFormat,
		Offered: nonNil(cfg.Offered), Formats: cfg.Formats,
		PacksRequired: nonNil(cfg.Packs), PacksLinked: nonNil(cfg.LinkedPacks), Problems: []string{},
	}
	if err != nil {
		r.Problems = append(r.Problems, err.Error())
	}
	json.NewEncoder(stdout).Encode(r)
	fmt.Fprintln(stderr, "RASTRILLO_CHECK_CONFIG=1: checked the configuration and exited without serving; unset it to serve")
	if err != nil {
		return true, 1
	}
	return true, 0
}

// nonNil keeps an empty list a JSON array: doctor and scripts read
// these fields as lists, and null would make them special-case it.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
