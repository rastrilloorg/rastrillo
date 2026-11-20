package main

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// gradeCheck runs a task's check in the graded copy of the app. The
// check reads the scaffold's copy as SKILLEVAL_BASE_DIR, and calls this
// binary back as SKILLEVAL_BIN for what a shell pattern cannot read.
func gradeCheck(ctx context.Context, dir, graded, tasksDir, task,
	scaffoldCopy string) bool {
	self, _ := os.Executable()
	environment := []string{"SKILLEVAL_BASE_DIR=" + scaffoldCopy,
		"SKILLEVAL_BIN=" + self}
	return grade(ctx, dir, graded, filepath.Join(dir, "check.log"),
		5*time.Minute, environment, "sh",
		filepath.Join(tasksDir, task+".check.sh"))
}
