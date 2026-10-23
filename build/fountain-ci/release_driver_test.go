// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package fountainci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReleaseDriver(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "tools", "fountain-release-run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const other = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, tc := range []struct {
		name          string
		args          []string
		remoteBranch  string
		localTag      string
		remoteTag     string
		annotatedTag  bool
		watchResult   int
		timeoutResult int
		pushResult    int
		missingRun    bool
		dispatchURL   string
		runCommit     string
		exit          int
		message       string
		publish       bool
		dispatch      bool
	}{
		{name: "dry-run", args: []string{"--dry-run", "--yes"}, dispatch: true, message: "validated from"},
		{name: "failed-workflow", args: []string{"--dry-run", "--yes"}, watchResult: 1, dispatch: true, exit: 1, message: "Release run failed:"},
		{name: "timeout", args: []string{"--dry-run", "--yes"}, timeoutResult: 124, dispatch: true, exit: 2, message: "Watch timed out; the run continues"},
		{name: "killed-watch", args: []string{"--dry-run", "--yes"}, timeoutResult: 137, dispatch: true, exit: 2, message: "Watch timed out; the run continues"},
		{name: "missing-exact-run", args: []string{"--dry-run", "--yes"}, missingRun: true, dispatch: true, exit: 1, message: "dispatch did not return one exact run URL"},
		{name: "foreign-run", args: []string{"--dry-run", "--yes"}, dispatchURL: "https://github.com/foreign/fountain/actions/runs/999", dispatch: true, exit: 1, message: "dispatch did not return one exact run URL"},
		{name: "malformed-run", args: []string{"--dry-run", "--yes"}, dispatchURL: "https://github.com/fixture/fountain/actions/runs/42?other=999", dispatch: true, exit: 1, message: "dispatch did not return one exact run URL"},
		{name: "multiple-runs", args: []string{"--dry-run", "--yes"}, dispatchURL: "https://github.com/fixture/fountain/actions/runs/42\nhttps://github.com/fixture/fountain/actions/runs/999", dispatch: true, exit: 1, message: "dispatch did not return one exact run URL"},
		{name: "competing-dispatch", args: []string{"--dry-run", "--yes"}, dispatch: true, message: "Watching https://github.com/fixture/fountain/actions/runs/42"},
		{name: "branch-moved-before-dispatch", args: []string{"--dry-run", "--yes"}, runCommit: other, dispatch: true, exit: 1, message: "dispatched run uses"},
		{name: "unpublished-dry-run", args: []string{"--dry-run", "--yes"}, remoteBranch: other, exit: 2, message: "qualify and push the candidate separately"},
		{name: "confirmation-before-push", remoteBranch: other, exit: 2, message: "refusing to run without --yes"},
		{name: "local-tag-conflict", args: []string{"--yes"}, localTag: other, exit: 2, message: "immutable tag"},
		{name: "remote-tag-conflict", args: []string{"--yes"}, remoteTag: other, exit: 2, message: "immutable remote tag"},
		{name: "immutable-version", args: []string{"--yes", "--move-tag"}, exit: 2, message: "version tags are immutable"},
		{name: "publish", args: []string{"--yes"}, remoteBranch: other, publish: true, dispatch: true, message: "Published:"},
		{name: "existing-tag", args: []string{"--yes"}, localTag: commit, remoteTag: commit, dispatch: true, message: "Published:"},
		{name: "existing-annotated-tag", args: []string{"--yes"}, remoteTag: commit, annotatedTag: true, dispatch: true, message: "Published:"},
		{name: "failed-branch-push", args: []string{"--yes"}, remoteBranch: other, pushResult: 1, publish: true, exit: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "tools"), 0o700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"tools/fountain-check-clean.sh": "#!/bin/sh\nexit 0\n",
				"git": `#!/bin/bash
printf 'git %s\n' "$*" >> "$DRIVER_LOG"
case "$1 $2" in
  'rev-parse --show-toplevel') echo "$DRIVER_ROOT" ;;
  'branch --show-current') echo codex/fountain-extensions ;;
  'rev-parse HEAD') echo "$DRIVER_COMMIT" ;;
  'rev-parse --verify') [[ -n $DRIVER_LOCAL_TAG ]] || exit 1; echo "$DRIVER_LOCAL_TAG" ;;
  'rev-parse v'*) echo "$DRIVER_LOCAL_TAG" ;;
  'ls-remote origin')
    if [[ $3 == refs/heads/* ]]; then
      printf '%s\t%s\n' "$DRIVER_REMOTE_BRANCH" "$3"
    elif [[ -n $DRIVER_REMOTE_TAG ]]; then
      if [[ $DRIVER_ANNOTATED_TAG == true ]]; then
        printf '%s\t%s\n%s\t%s\n' annotated-object "$3" "$DRIVER_REMOTE_TAG" "$4"
      else
        printf '%s\t%s\n' "$DRIVER_REMOTE_TAG" "$3"
      fi
    fi ;;
  'push origin') exit "$DRIVER_PUSH_RESULT" ;;
  'tag '*) ;;
  *) printf 'unexpected git command: %s\n' "$*" >&2; exit 99 ;;
esac
`,
				"gh": `#!/bin/bash
printf 'gh %s\n' "$*" >> "$DRIVER_LOG"
case "$1 $2" in
  'repo view') echo fixture/fountain ;;
  'workflow run')
    if [[ $DRIVER_MISSING_RUN == true ]]; then
      echo 'Created workflow_dispatch event'
    elif [[ -n $DRIVER_DISPATCH_URL ]]; then
      printf '%s\n' "$DRIVER_DISPATCH_URL"
    else
      echo https://github.com/fixture/fountain/actions/runs/42
    fi ;;
  'run list') echo 999 ;; # A newer concurrent dispatch must never be selected.
  'run watch')
    [[ $3 == 42 ]] || exit 99
    [[ " $* " == *' --exit-status '* ]] || exit 0
    exit "$DRIVER_WATCH_RESULT" ;;
  'run view')
    [[ $3 == 42 ]] || exit 99
    if [[ " $* " == *' --json headSha '* ]]; then
      echo "${DRIVER_RUN_COMMIT:-$DRIVER_COMMIT}"
    else
      echo '##[error]fixture workflow failed'
    fi ;;
  'release view') echo 'Published: https://github.com/fixture/fountain/releases/tag/v15.0.10-fountain.3' ;;
  *) printf 'unexpected gh command: %s\n' "$*" >&2; exit 99 ;;
esac
`,
				"timeout": `#!/bin/bash
printf 'timeout %s\n' "$*" >> "$DRIVER_LOG"
if (( DRIVER_TIMEOUT_RESULT != 0 )); then exit "$DRIVER_TIMEOUT_RESULT"; fi
while [[ $1 == --* ]]; do shift; done
shift
exec "$@"
`,
				"sleep": "#!/bin/sh\nexit 0\n",
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			remoteBranch := tc.remoteBranch
			if remoteBranch == "" {
				remoteBranch = commit
			}
			logPath := filepath.Join(root, "commands")
			cmd := exec.CommandContext(t.Context(), "bash", append([]string{script, "15.0.10-fountain.3"}, tc.args...)...)
			cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
				"DRIVER_ROOT="+root, "DRIVER_LOG="+logPath, "DRIVER_COMMIT="+commit,
				"DRIVER_REMOTE_BRANCH="+remoteBranch, "DRIVER_LOCAL_TAG="+tc.localTag,
				"DRIVER_REMOTE_TAG="+tc.remoteTag, "DRIVER_ANNOTATED_TAG="+strconv.FormatBool(tc.annotatedTag),
				"DRIVER_MISSING_RUN="+strconv.FormatBool(tc.missingRun),
				"DRIVER_DISPATCH_URL="+tc.dispatchURL, "DRIVER_RUN_COMMIT="+tc.runCommit,
				"DRIVER_WATCH_RESULT="+strconv.Itoa(tc.watchResult), "DRIVER_TIMEOUT_RESULT="+strconv.Itoa(tc.timeoutResult),
				"DRIVER_PUSH_RESULT="+strconv.Itoa(tc.pushResult))
			output, err := cmd.CombinedOutput()
			exit := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exit = exitErr.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != tc.exit || !strings.Contains(string(output), tc.message) {
				t.Fatalf("exit %d, want %d; output: %s", exit, tc.exit, output)
			}
			commands, err := os.ReadFile(logPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			calls := string(commands)
			if dispatch := strings.Contains(calls, "gh workflow run "); dispatch != tc.dispatch {
				t.Fatalf("dispatch = %t, want %t:\n%s", dispatch, tc.dispatch, calls)
			}
			if !tc.publish && (strings.Contains(calls, "git push ") || strings.Contains(calls, "git tag ")) {
				t.Fatalf("unexpected ref mutation:\n%s", calls)
			}
			if strings.Contains(calls, "gh run list ") || strings.Contains(calls, "gh run watch 999 ") {
				t.Fatalf("driver looked up another dispatch instead of its created run:\n%s", calls)
			}
			if strings.Contains(calls, "git push -f") || strings.Contains(calls, "git tag -f") {
				t.Fatalf("version tags must never be forced:\n%s", calls)
			}
			if tc.exit != 0 && strings.Contains(string(output), "validated from") {
				t.Fatalf("failed run reported validation success: %s", output)
			}
			if tc.name == "dry-run" && !strings.Contains(calls, "-F dry_run=true") {
				t.Fatalf("dry-run dispatched publication:\n%s", calls)
			}
			if tc.name == "publish" && !strings.Contains(calls, "git push origin refs/tags/v15.0.10-fountain.3") {
				t.Fatalf("publication did not push its immutable tag:\n%s", calls)
			}
		})
	}
}
