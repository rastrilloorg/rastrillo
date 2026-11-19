package main

import (
	"strings"
	"testing"
)

func TestMostCommonReportsATie(t *testing.T) {
	winner, tied := mostCommon([]string{"Render", "render", "Fly.io", "Vercel", "Vercel"})
	if winner != "" || strings.Join(tied, ",") != "Render,Vercel" {
		t.Errorf("mostCommon = %q, %v; want no winner and Render, Vercel tied",
			winner, tied)
	}
	winner, tied = mostCommon([]string{"Fly.io", "Render", "fly.io"})
	if winner != "Fly.io" || tied != nil {
		t.Errorf("mostCommon = %q, %v; want Fly.io", winner, tied)
	}
}
