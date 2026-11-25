package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/calibrate"
)

// calibrateCommand is `bench calibrate`: serve the blind pairs for Paul
// to judge, writing each judgement to results/<out>.json as it is made.
func calibrateCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	pairsPath := flags.String("pairs", "", "results/pairs-<date>.json")
	listenIP := flags.String("listen-ip", "", "127.0.0.1 or this "+
		"machine's Tailscale IP ($(tailscale ip -4)); never all interfaces")
	port := flags.Int("port", 8790, "the page's port")
	out := flags.String("out", "", "label for results/<label>.json "+
		"(default calibration-<date>, the date of -pairs); serving it "+
		"again resumes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *pairsPath == "" || *listenIP == "" {
		return errors.New("usage: calibrate -pairs results/pairs-<date>.json " +
			"-listen-ip <ip> [-port 8790] [-out calibration-<date>]")
	}
	address := net.JoinHostPort(*listenIP, strconv.Itoa(*port))
	if err := calibrate.CheckAddress(address); err != nil {
		return err
	}
	if *out == "" {
		*out = labelFor(*pairsPath, "calibration")
	}
	if err := checkLabel(*out); err != nil {
		return err
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	chosen, err := loadPairsFile(*pairsPath)
	if err != nil {
		return err
	}
	path := resultPath(root, *out, ".json")
	if err := startCalibration(path, *pairsPath, chosen.Seed); err != nil {
		return err
	}
	fmt.Printf("Judge the pairs at http://%s/\n", address)
	fmt.Println("Judgements go to", path, "as they are made.")
	if err := calibrate.Serve(ctx, address, chosen.Pairs, path); err != nil {
		return err
	}
	calibration, err := readCalibration(path)
	if err != nil {
		return err
	}
	fmt.Print(judgedTally(chosen.Pairs, verdictsOf(calibration)))
	return nil
}

// loadPairsFile reads a pairs file chosen for calibration, refusing
// one made with -across: those compare arms, not taste.
func loadPairsFile(path string) (pairsFile, error) {
	var chosen pairsFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return chosen, err
	}
	if err := json.Unmarshal(raw, &chosen); err != nil {
		return chosen, fmt.Errorf("%s: %w", path, err)
	}
	if chosen.Across || len(chosen.Pairs) == 0 {
		return chosen, fmt.Errorf("%s has no calibration pairs", path)
	}
	return chosen, nil
}

// startCalibration writes a calibration with no judgements yet,
// recording the pairs file and its seed, so that `bench pairs -judged`
// can read it. One already there is resumed, if it was made on the
// same pairs file with the same seed.
func startCalibration(path, pairsPath string, seed int64) error {
	if _, err := os.Stat(path); err == nil {
		made, err := readCalibration(path)
		if err != nil {
			return err
		}
		if made.Seed != seed ||
			filepath.Base(made.Pairs) != filepath.Base(pairsPath) {
			return fmt.Errorf("%s judges %s (seed %d), not %s (seed %d): "+
				"pass another -out", path, made.Pairs, made.Seed, pairsPath,
				seed)
		}
		return nil
	}
	encoded, err := json.MarshalIndent(calibrate.Calibration{
		Pairs: pairsPath, Seed: seed}, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(path, append(encoded, '\n'))
}

func readCalibration(path string) (calibrate.Calibration, error) {
	var calibration calibrate.Calibration
	raw, err := os.ReadFile(path)
	if err != nil {
		return calibration, err
	}
	if err := json.Unmarshal(raw, &calibration); err != nil {
		return calibration, fmt.Errorf("%s: %w", path, err)
	}
	return calibration, nil
}

func verdictsOf(calibration calibrate.Calibration) map[int]calibrate.Verdict {
	verdicts := map[int]calibrate.Verdict{}
	for _, judged := range calibration.Judgements {
		verdicts[judged.PairID] = judged.Verdict
	}
	return verdicts
}
