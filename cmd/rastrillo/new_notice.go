package main

import (
	"fmt"
	"strings"
)

// printIconNotice gives informed consent, stated once. A supported
// choice that nags on every build is not really supported.
func printIconNotice(notice, iconDelivery string) {
	if notice == "" {
		return
	}
	fmt.Printf("\nnote: --icon-delivery=%s\n", iconDelivery)
	lines := strings.Split(strings.TrimRight(notice, "\n"), "\n")
	for _, line := range lines {
		fmt.Println("  " + line)
	}
}
