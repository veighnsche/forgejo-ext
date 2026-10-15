// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package fountainci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAggregateAdmission(t *testing.T) {
	lanes := []string{"SOURCE", "UNIT", "DATABASE", "BROWSER", "PACKAGING"}
	run := func(changed, result string) bool {
		t.Helper()
		cmd := exec.Command("bash", filepath.Join("..", "..", "tools", "fountain-ci-gate.sh"))
		for _, variable := range os.Environ() {
			if !strings.HasSuffix(strings.SplitN(variable, "=", 2)[0], "_RESULT") {
				cmd.Env = append(cmd.Env, variable)
			}
		}
		for _, lane := range lanes {
			value := "success"
			if lane == changed {
				value = result
			}
			if value != "missing" {
				cmd.Env = append(cmd.Env, lane+"_RESULT="+value)
			}
		}
		return cmd.Run() == nil
	}
	if !run("", "") {
		t.Fatal("all successful lanes must qualify")
	}
	for _, lane := range lanes {
		for _, outcome := range []string{"failure", "cancelled", "skipped", "neutral", "", "missing"} {
			t.Run(lane+"/"+outcome, func(t *testing.T) {
				if run(lane, outcome) {
					t.Fatal("unsuccessful lane qualified")
				}
			})
		}
	}
}

func TestRequiredTestResults(t *testing.T) {
	for name, events := range map[string]string{
		"empty":        "",
		"package-only": `{"Action":"pass","Package":"fixture"}`,
		"skipped":      `{"Action":"skip","Package":"fixture","Test":"TestRequired"}`,
		"incomplete":   `{"Action":"run","Package":"fixture","Test":"TestRequired"}`,
		"failed":       `{"Action":"fail","Package":"fixture","Test":"TestRequired"}`,
		"malformed":    "not JSON",
		"passed":       `{"Action":"pass","Package":"fixture","Test":"TestRequired"}`,
	} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "results.jsonl")
			if err := os.WriteFile(file, []byte(events), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("node", filepath.Join("..", "..", "tools", "fountain-test-results.mjs"), file)
			if passed := cmd.Run() == nil; passed != (name == "passed") {
				t.Fatalf("unexpected admission: %v", passed)
			}
		})
	}
}

func TestSourceSnapshotDrift(t *testing.T) {
	checker, err := filepath.Abs(filepath.Join("..", "..", "tools", "fountain-check-clean.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"clean", "worktree", "index", "untracked"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			git := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = root
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git: %v: %s", err, output)
				}
			}
			write := func(name, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			git("init", "-q")
			write("fixture", "original\n")
			git("add", ".")
			git("-c", "user.name=fountain-check", "-c", "user.email=fountain-check@example.invalid", "commit", "-qm", "Fixture")
			switch state {
			case "worktree":
				write("fixture", "changed\n")
			case "index":
				write("fixture", "staged\n")
				git("add", "fixture")
				write("fixture", "original\n")
			case "untracked":
				write("new-source", "unexpected\n")
			}
			cmd := exec.Command("bash", checker)
			cmd.Dir = root
			if passed := cmd.Run() == nil; passed != (state == "clean") {
				t.Fatalf("unexpected source admission: %v", passed)
			}
		})
	}
}
