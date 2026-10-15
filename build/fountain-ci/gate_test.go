// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package fountainci

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAggregateAdmission(t *testing.T) {
	lanes := []string{"SOURCE", "UNIT", "DATABASE", "BROWSER", "PACKAGING"}
	run := func(changed, result string, reused bool, started int64) bool {
		t.Helper()
		cmd := exec.Command("bash", filepath.Join("..", "..", "tools", "fountain-ci-gate.sh"))
		for _, variable := range os.Environ() {
			key := strings.SplitN(variable, "=", 2)[0]
			if !strings.HasSuffix(key, "_RESULT") && key != "REUSED_CHECKS" && key != "GATE_STARTED_AT" && key != "FOUNTAIN_CI_PROFILE" {
				cmd.Env = append(cmd.Env, variable)
			}
		}
		cmd.Env = append(cmd.Env, "FOUNTAIN_CI_PROFILE=push", "REUSED_CHECKS="+strconv.FormatBool(reused), "GATE_STARTED_AT="+strconv.FormatInt(started, 10))
		for _, lane := range append(append([]string{}, lanes...), "REUSE") {
			value := "success"
			if reused && lane != "REUSE" {
				value = "skipped"
			}
			if lane == changed {
				value = result
			}
			if value != "missing" {
				cmd.Env = append(cmd.Env, lane+"_RESULT="+value)
			}
		}
		return cmd.Run() == nil
	}
	now := time.Now().Unix()
	if !run("", "", false, now) || !run("", "", true, now) {
		t.Fatal("all successful lanes must qualify")
	}
	for _, lane := range append(append([]string{}, lanes...), "REUSE") {
		for _, outcome := range []string{"failure", "cancelled", "skipped", "neutral", "", "missing"} {
			t.Run(lane+"/"+outcome, func(t *testing.T) {
				if run(lane, outcome, false, now) {
					t.Fatal("unsuccessful lane qualified")
				}
			})
		}
	}
	for _, lane := range lanes {
		if run(lane, "success", true, now) || run(lane, "failure", true, now) {
			t.Fatalf("reuse requires every lane to be skipped: %s", lane)
		}
	}
	for _, started := range []int64{now - 300, now + 60} {
		if run("", "", false, started) || run("", "", true, started) {
			t.Fatal("expired or future qualification must not pass")
		}
	}
}

func TestExactCommitReuse(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const receipt = `{"workflow_runs":[{"id":17,"head_sha":"` + sha + `","head_branch":"ci/fountain-` + sha + `","event":"push","path":".github/workflows/fountain-ci.yml","status":"completed","conclusion":"success"}]}`
	script, err := filepath.Abs(filepath.Join("..", "..", "tools", "fountain-ci-reuse.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for name, response := range map[string]string{
		"exact":              receipt,
		"different-sha":      strings.Replace(receipt, `"head_sha":"`+sha+`"`, `"head_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`, 1),
		"different-tag":      strings.Replace(receipt, "ci/fountain-"+sha, "ci/fountain-old", 1),
		"different-workflow": strings.Replace(receipt, "fountain-ci.yml", "unrelated.yml", 1),
		"failed":             strings.Replace(receipt, "success", "failure", 1),
		"pending":            strings.Replace(receipt, "completed", "in_progress", 1),
		"missing":            `{"workflow_runs":[]}`,
		"malformed":          `not JSON`,
		"pr":                 receipt,
		"different-ref":      receipt,
		"rerun":              receipt,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join(root, "response.json")
			output := filepath.Join(root, "output")
			if err := os.WriteFile(fixture, []byte(response), 0o600); err != nil {
				t.Fatal(err)
			}
			fakeGH := "#!/bin/sh\ncase \"$2\" in\n  */actions/runs/42/attempts/*) date -u +%Y-%m-%dT%H:%M:%SZ ;;\n  *) cat \"$REUSE_FIXTURE\" ;;\nesac\n"
			if err := os.WriteFile(filepath.Join(root, "gh"), []byte(fakeGH), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", script)
			event, ref, attempt := "push", "refs/heads/codex/fountain-extensions", "1"
			switch name {
			case "pr":
				event = "pull_request"
			case "different-ref":
				ref = "refs/heads/unrelated"
			case "rerun":
				attempt = "2"
			}
			cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GITHUB_REPOSITORY=fixture/repo", "GITHUB_RUN_ID=42", "GITHUB_EVENT_NAME="+event, "GITHUB_REF="+ref,
				"GITHUB_RUN_ATTEMPT="+attempt, "GITHUB_SHA="+sha, "GITHUB_OUTPUT="+output, "REUSE_FIXTURE="+fixture)
			if result, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("reuse: %v: %s", err, result)
			}
			result, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if reused := strings.Contains(string(result), "reused=true\n"); reused != (name == "exact") {
				t.Fatalf("unexpected receipt admission: %s", result)
			}
		})
	}
}

func TestPushCommandDeadline(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "tools", "fountain-push.sh"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "git"), []byte("#!/bin/sh\nsleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "FOUNTAIN_PUSH_BOUNDED=", "FOUNTAIN_PUSH_TIMEOUT_SECONDS=2", "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
	started := time.Now()
	output, err := cmd.CombinedOutput()
	if err == nil || time.Since(started) > 3*time.Second || !strings.Contains(string(output), "Push budget expired") {
		t.Fatalf("publisher did not enforce deadline: %v after %s: %s", err, time.Since(started), output)
	}
	for _, budget := range []string{"0", "301", "invalid"} {
		cmd := exec.Command("bash", script)
		cmd.Env = append(os.Environ(), "FOUNTAIN_PUSH_TIMEOUT_SECONDS="+budget)
		if err := cmd.Run(); err == nil {
			t.Fatalf("invalid budget admitted: %s", budget)
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
