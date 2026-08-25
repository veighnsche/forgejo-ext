// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// actorActivityWatermark tracks, per remote actor, the most recent activity
// start time that has been processed. This replaces the host-wide
// FederationHost.LatestActivity watermark:
//
//   - ordering is enforced per actor, so two different actors on the same
//     host no longer conflict;
//   - a malicious actor can only advance its own watermark, not the whole
//     host's (the previous design allowed a spoofed actor with a far-future
//     start time to deny activity processing for every actor on the host);
//   - the actor identity is the one verified from the request signature
//     (see actor_binding.go), so an unauthenticated peer cannot advance
//     another actor's watermark.
//
// The cache is bounded: entries are dropped after maxWatermarkAge so the map
// does not grow without bound. Replay of a captured request within the
// signature Date window (maxSignatureAge) is still rejected because the
// replayed activity's start time is not newer than the recorded watermark.
var actorActivityWatermark *lru.Cache[string, time.Time]

func init() {
	var err error
	actorActivityWatermark, err = lru.New[string, time.Time](100_000)
	if err != nil {
		panic(err) // 100k entries with two fixed-size values cannot fail
	}
}

// activitySeenByActor records the activity start time for the actor and
// reports whether the activity is a replay (i.e. its start time is not newer
// than the most recent one already recorded for the same actor).
func activitySeenByActor(actorURI string, startTime time.Time) bool {
	lock := sync.Mutex{}
	lock.Lock()
	defer lock.Unlock()

	last, ok := actorActivityWatermark.Get(actorURI)
	if ok && !startTime.After(last) {
		return true // replay or out-of-order
	}
	actorActivityWatermark.Add(actorURI, startTime)
	return false
}
