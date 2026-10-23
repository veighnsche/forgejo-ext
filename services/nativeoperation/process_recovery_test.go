//go:build linux

// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"encoding/json" //nolint:depguard // Persist the exact native intent across process death.
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	nativeoperation_model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"

	"code.forgejo.org/xorm/xorm"
	"code.forgejo.org/xorm/xorm/names"
	"github.com/stretchr/testify/require"
)

const processRecoveryDirectoryEnv = "FOUNTAIN_ISSUE_GRAPH_PROCESS_RECOVERY_DIRECTORY"

func openProcessRecoveryDatabase(t *testing.T, path string) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite3", "file:"+path+"?_txlock=immediate&_busy_timeout=10000")
	require.NoError(t, err)
	engine.SetMapper(names.GonicMapper{})
	db.SetDefaultEngine(context.Background(), engine)
	return engine
}

// TestIssueGraphProcessRecoveryWorker attaches only to the parent's existing
// file. TestMain's independent in-memory fixture never initializes this file.
// The parent SIGKILLs this exact child at the production submission barrier.
func TestIssueGraphProcessRecoveryWorker(t *testing.T) {
	root := os.Getenv(processRecoveryDirectoryEnv)
	if root == "" {
		return
	}
	engine := openProcessRecoveryDatabase(t, filepath.Join(root, "native.sqlite"))
	t.Cleanup(func() { _ = engine.Close() })
	setting.AppDataPath = filepath.Join(root, "appdata")
	encoded, err := os.ReadFile(filepath.Join(root, "intent.json"))
	require.NoError(t, err)
	var intent ValidIntent
	require.NoError(t, json.Unmarshal(encoded, &intent))
	decision := graphDecision(t)
	_, err = NewService().Submit(t.Context(), decision, operationTestInstallation, &intent)
	t.Fatalf("submission returned before the parent killed its blocked child: %v", err)
}

// TestIssueGraphProcessDeathRecovery proves actual process death and reopening
// persistent SQLite state, before and after the graph's atomic primary commit.
// It does not simulate the crash with a barrier timeout, and does not establish
// merge, receive, PostgreSQL or installed-appliance shutdown behavior.
func TestIssueGraphProcessDeathRecovery(t *testing.T) {
	for _, point := range []string{CrashPointIssueGraphBeforePrimary, CrashPointIssueGraphAfterPrimary} {
		t.Run(point, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"appdata", "barrier", "worker-tmp"} {
				require.NoError(t, os.Mkdir(filepath.Join(root, dir), 0o700))
			}
			previous := db.DefaultContext.(db.Engined).Engine()
			previousAppData := setting.AppDataPath
			setting.AppDataPath = filepath.Join(root, "appdata")
			engine := openProcessRecoveryDatabase(t, filepath.Join(root, "native.sqlite"))
			t.Cleanup(func() {
				db.SetDefaultEngine(context.Background(), previous)
				setting.AppDataPath = previousAppData
				require.NoError(t, unittest.InitFixtures(unittest.FixturesOptions{Dir: unittest.FixturesDir()}))
				if engine != nil {
					require.NoError(t, engine.Close())
				}
			})
			require.NoError(t, db.SyncAllTables())
			require.NoError(t, unittest.InitFixtures(unittest.FixturesOptions{Dir: unittest.FixturesDir()}))
			require.NoError(t, unittest.LoadFixtures())
			decision := graphDecision(t)
			intent := graphIntent(t, "graph-process-death", graphPayload())
			encoded, err := json.Marshal(intent)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, "intent.json"), encoded, 0o600))
			countIssues := func() int64 {
				count, err := db.GetEngine(t.Context()).Where("repo_id=?", 1).Count(new(issues_model.Issue))
				require.NoError(t, err)
				return count
			}
			countEdges := func() int64 {
				count, err := db.GetEngine(t.Context()).Count(new(issues_model.IssueDependency))
				require.NoError(t, err)
				return count
			}
			beforeIssues, beforeEdges := countIssues(), countEdges()
			require.NoError(t, engine.Close())
			engine = nil

			log, err := os.Create(filepath.Join(root, "worker.log"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = log.Close() })
			child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestIssueGraphProcessRecoveryWorker$", "-test.v", "-test.timeout=90s")
			child.Env = append(os.Environ(),
				processRecoveryDirectoryEnv+"="+root,
				"TMPDIR="+filepath.Join(root, "worker-tmp"),
				"NATIVEOP_TEST_CRASH_POINT="+point,
				"NATIVEOP_TEST_CRASH_BARRIER_DIR="+filepath.Join(root, "barrier"),
				"NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS=60")
			child.Stdout, child.Stderr = log, log
			require.NoError(t, child.Start())
			exited := make(chan error, 1)
			go func() { exited <- child.Wait() }()
			reaped := false
			t.Cleanup(func() {
				if !reaped {
					_ = child.Process.Kill()
					<-exited
				}
			})
			entered := filepath.Join(root, "barrier", point+".entered")
			reached := false
			deadline := time.After(30 * time.Second)
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for !reached {
				select {
				case err := <-exited:
					reaped = true
					output, _ := os.ReadFile(filepath.Join(root, "worker.log"))
					t.Fatalf("child exited before barrier: %v\n%s", err, output)
				case <-deadline:
					output, _ := os.ReadFile(filepath.Join(root, "worker.log"))
					t.Fatalf("child never reached %s\n%s", point, output)
				case <-tick.C:
					_, err := os.Stat(entered)
					reached = err == nil
				}
			}
			require.NoError(t, child.Process.Kill())
			var killed *exec.ExitError
			select {
			case err := <-exited:
				reaped = true
				require.True(t, errors.As(err, &killed), "child must exit because it was killed")
			case <-time.After(10 * time.Second):
				t.Fatal("killed child did not exit")
			}
			status, ok := killed.ProcessState.Sys().(syscall.WaitStatus)
			require.True(t, ok)
			require.True(t, status.Signaled())
			require.Equal(t, syscall.SIGKILL, status.Signal())
			t.Logf("owned child pid=%d reached %s and was reaped after SIGKILL", child.Process.Pid, point)

			// No writer remains: the child has died, and the original connection
			// was closed before launch. Reopen without resetting any fixture rows.
			engine = openProcessRecoveryDatabase(t, filepath.Join(root, "native.sqlite"))
			held, err := nativeoperation_model.ReadReservation(t.Context())
			require.NoError(t, err)
			require.Equal(t, conditionalOwner(operationTestInstallation, intent.OperationID), held.Owner)
			require.Positive(t, held.Generation)
			interrupted, err := nativeoperation_model.LookupOperation(t.Context(), operationTestInstallation, intent.OperationID)
			require.NoError(t, err)
			require.NotNil(t, interrupted)
			committed := point == CrashPointIssueGraphAfterPrimary
			require.Equal(t, committed, interrupted.Admitted)
			if committed {
				require.Equal(t, nativeoperation_model.EffectCommitted, interrupted.EffectState)
				require.NotEmpty(t, interrupted.Receipt)
				require.Equal(t, beforeIssues+2, countIssues())
				require.Equal(t, beforeEdges+1, countEdges())
			} else {
				require.Equal(t, nativeoperation_model.EffectPending, interrupted.EffectState)
				require.Empty(t, interrupted.Receipt)
				require.Equal(t, beforeIssues, countIssues())
				require.Equal(t, beforeEdges, countEdges())
			}
			inhibitDomain(t)
			svc := NewService()
			assessment, err := svc.Recover(t.Context(), held.Owner, held.Generation)
			require.NoError(t, err)
			require.Equal(t, RecoveryReleased, assessment.Verdict)
			record, err := nativeoperation_model.LookupOperation(t.Context(), operationTestInstallation, intent.OperationID)
			require.NoError(t, err)
			if committed {
				require.Equal(t, nativeoperation_model.EffectCommitted, assessment.Effect)
				require.Equal(t, nativeoperation_model.CompletionNeedsIntervention, record.Completion)
				require.Equal(t, interrupted.Receipt, record.Receipt)
				var receipt sdk.IssueGraphReceipt
				require.NoError(t, json.Unmarshal([]byte(record.Receipt), &receipt))
				require.Equal(t, int64(2), receipt.ActorID)
				require.Equal(t, int64(1), receipt.RepositoryID)
				require.Len(t, receipt.Issues, 2)
				require.Len(t, receipt.Dependencies, 1)
				for i, entry := range receipt.Issues {
					issue, err := issues_model.GetIssueByID(t.Context(), entry.IssueID)
					require.NoError(t, err)
					require.Equal(t, intent.IssueGraph.Creates[i].Key, entry.Key)
					require.Equal(t, intent.IssueGraph.Creates[i].Title, issue.Title)
					require.Equal(t, intent.IssueGraph.Creates[i].Body, issue.Content)
					require.Equal(t, receipt.ActorID, issue.PosterID)
				}
				edge := receipt.Dependencies[0]
				require.Equal(t, receipt.Issues[1].IssueID, edge.IssueID)
				require.Equal(t, receipt.Issues[0].IssueID, edge.PrerequisiteID)
				require.False(t, edge.Removed)
			} else {
				require.Equal(t, nativeoperation_model.EffectNotCommitted, assessment.Effect)
				require.Equal(t, nativeoperation_model.ReasonRecoveredNoEffect, record.Reason)
			}
			idle, err := nativeoperation_model.ReadReservation(t.Context())
			require.NoError(t, err)
			require.Empty(t, idle.Owner)
			issues, edges := countIssues(), countEdges()
			replay, err := svc.Submit(t.Context(), decision, operationTestInstallation, intent)
			require.NoError(t, err)
			require.Equal(t, record.EffectState, replay.EffectState)
			require.Equal(t, record.Receipt, string(replay.Receipt))
			require.Equal(t, issues, countIssues(), "replay cannot create duplicate issues")
			require.Equal(t, edges, countEdges(), "replay cannot create duplicate dependencies")
		})
	}
}
