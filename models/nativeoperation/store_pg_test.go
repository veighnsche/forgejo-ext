// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"code.forgejo.org/xorm/xorm"
	"code.forgejo.org/xorm/xorm/names"
	"forgejo.org/models/db"
	model "forgejo.org/models/nativeoperation"
	execcontext "forgejo.org/modules/nativeoperation"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for the disposable PostgreSQL fixture

	"github.com/stretchr/testify/require"
)

// useDisposablePostgres connects the real claim code to a disposable
// PostgreSQL and swaps it in as the default engine for one test, restoring
// the shared unit-test engine afterwards. It skips without
// FORGEJO_TEST_PG_DSN, so ordinary runs stay on SQLite.
func useDisposablePostgres(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("FORGEJO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("FORGEJO_TEST_PG_DSN is not set; needs a disposable PostgreSQL")
	}
	engine, err := xorm.NewEngine("pgx", dsn)
	require.NoError(t, err)
	engine.SetMapper(names.GonicMapper{})
	t.Cleanup(func() { _ = engine.Close() })
	require.NoError(t, engine.Sync2(new(model.Operation), new(model.Reservation)))
	_, err = engine.Exec("DELETE FROM `operation`")
	require.NoError(t, err)
	_, err = engine.Exec("DELETE FROM `reservation`")
	require.NoError(t, err)

	previous, ok := db.DefaultContext.(db.Engined)
	require.True(t, ok, "default context carries the unit-test engine")
	db.SetDefaultEngine(context.Background(), engine)
	t.Cleanup(func() { db.SetDefaultEngine(context.Background(), previous.Engine()) })
}

type raceResult struct {
	owner string
	won   bool
	err   error
}

// runClaimRace fires racers claimants at the idle reservation through
// separate sessions, the way concurrent processes reach PostgreSQL.
func runClaimRace(racers int, claim func(ctx context.Context, i int) (owner string, err error)) []raceResult {
	gate := make(chan struct{})
	results := make([]raceResult, racers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			owner, err := claim(context.Background(), i)
			results[i] = raceResult{owner: owner, won: err == nil, err: err}
		}()
	}
	close(gate)
	wg.Wait()
	return results
}

func winnersOf(results []raceResult) []raceResult {
	var winners []raceResult
	for _, r := range results {
		if r.won {
			winners = append(winners, r)
		}
	}
	return winners
}

func TestConcurrentOrdinaryClaimOnPostgres(t *testing.T) {
	useDisposablePostgres(t)
	ctx := context.Background()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, start.Owner)

	results := runClaimRace(8, func(ctx context.Context, i int) (string, error) {
		owner := fmt.Sprintf("ord:race/pg/%d", i)
		_, err := model.ClaimOrdinary(ctx, owner, `{"kind":"ordinary"}`, "v")
		return owner, err
	})
	for _, r := range results {
		if !r.won {
			require.ErrorIs(t, r.err, model.ErrBusy, "loser %s", r.owner)
		}
	}
	winners := winnersOf(results)
	require.Len(t, winners, 1, "exactly one claimant must gain the reservation")

	final, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, winners[0].owner, final.Owner)
	require.Equal(t, start.Revision+1, final.Revision)
	require.Equal(t, start.Generation+1, final.Generation)

	// Every loser stays fenced out of effects under the winner's hold.
	for _, r := range results {
		if r.won {
			continue
		}
		require.ErrorIs(t, model.ReleaseOwner(ctx, r.owner), model.ErrWrongOwner)
		loserCtx := execcontext.NewContext(ctx, &execcontext.Execution{Owner: r.owner, Generation: final.Generation})
		require.ErrorIs(t, model.RequireHeldOwnership(loserCtx), model.ErrBusy, "loser %s", r.owner)
	}
	require.NoError(t, model.ReleaseOwner(ctx, winners[0].owner))
}

func TestConcurrentConditionalClaimOnPostgres(t *testing.T) {
	useDisposablePostgres(t)
	ctx := context.Background()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, start.Owner)

	results := runClaimRace(8, func(ctx context.Context, i int) (string, error) {
		id := fmt.Sprintf("op-pg-race-%d", i)
		owner := "cond:" + testInstallation + "/" + id
		_, err := model.ClaimConditional(ctx, testOperation(id, start.Revision), owner, `{"kind":"conditional"}`, "v")
		return owner, err
	})
	for _, r := range results {
		if !r.won {
			require.True(t, r.err == model.ErrBusy || r.err == model.ErrStaleRevision, "loser %s: %v", r.owner, r.err)
		}
	}
	winners := winnersOf(results)
	require.Len(t, winners, 1, "exactly one claimant must gain the revision")

	final, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, winners[0].owner, final.Owner)
	require.Equal(t, start.Revision+1, final.Revision)

	// Losing claims record no operation and cannot admit under their name.
	for i, r := range results {
		id := fmt.Sprintf("op-pg-race-%d", i)
		stored, err := model.LookupOperation(ctx, testInstallation, id)
		require.NoError(t, err)
		if r.won {
			require.NotNil(t, stored, "winner records its operation")
			continue
		}
		require.Nil(t, stored, "loser %s records nothing", r.owner)
		_, _, err = model.RecordAdmissionAttempt(ctx, testInstallation, id, r.owner, true, "")
		require.Error(t, err, "loser %s cannot admit", r.owner)
	}
	require.NoError(t, model.ReleaseOwner(ctx, winners[0].owner))
}

func TestConcurrentPublishReceiveClaimOnPostgres(t *testing.T) {
	useDisposablePostgres(t)
	ctx := context.Background()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, start.Owner)
	_, err = model.InsertWaitingOperation(ctx, testOperation("op-pg-receive", start.Revision))
	require.NoError(t, err)

	results := runClaimRace(8, func(ctx context.Context, i int) (string, error) {
		owner := fmt.Sprintf("cond:%s/op-pg-receive#%d", testInstallation, i)
		_, _, err := model.ClaimPublishReceive(ctx, testInstallation, "op-pg-receive", owner, `{"kind":"conditional"}`, "v", 1000000000)
		return owner, err
	})
	for _, r := range results {
		if !r.won {
			require.True(t, r.err == model.ErrBusy || r.err == model.ErrStaleRevision || r.err == model.ErrAdmissionLost, "loser %s: %v", r.owner, r.err)
		}
	}
	winners := winnersOf(results)
	require.Len(t, winners, 1, "exactly one receiver must gain the reservation")

	final, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, winners[0].owner, final.Owner)
	require.Equal(t, start.Revision+1, final.Revision)
	require.NoError(t, model.ReleaseOwner(ctx, winners[0].owner))
}
