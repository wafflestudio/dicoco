package waffle

import (
	"bytes"
	"context"
	"log"
	"testing"
	"time"
)

func TestFirstAnnouncementAt(t *testing.T) {
	for _, tc := range []struct{ now, want string }{
		{"2026-09-18T12:00:00+09:00", "2026-09-23T10:00:00+09:00"},
		{"2026-09-23T09:59:00+09:00", "2026-09-23T10:00:00+09:00"},
		{"2026-09-23T10:00:00+09:00", "2026-09-23T10:00:00+09:00"},
		{"2026-09-23T10:01:00+09:00", "2026-09-30T10:00:00+09:00"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		want, _ := time.Parse(time.RFC3339, tc.want)
		if got := firstAnnouncementAt(now); !got.Equal(want) {
			t.Errorf("firstAnnouncementAt(%s) = %s, want %s", tc.now, got, want)
		}
	}
}

func TestScheduleSurvivesRestartAndMigratesLegacyTime(t *testing.T) {
	now := time.Date(2026, 9, 18, 3, 0, 0, 0, time.UTC)
	legacy := now.Add(announcementInterval)
	store := &memoryStateStore{state: State{
		Version: stateVersion, NextAnnouncementAt: &legacy,
		Recipients: map[string]int{"recipient": 3},
	}}
	check := func() {
		h := &Handler{store: store, now: func() time.Time { return now }, logger: log.New(&bytes.Buffer{}, "", 0)}
		if err := h.announceIfDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	check()
	want := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	now = now.Add(24 * time.Hour)
	check() // A new handler must keep the persisted deadline and counts.
	if !store.state.NextAnnouncementAt.Equal(want) || store.state.Recipients["recipient"] != 3 {
		t.Fatalf("unexpected state after restart: %+v", store.state)
	}
	if got := nextAnnouncementAfter(want, want.Add(time.Minute)); !got.Equal(want.Add(14 * 24 * time.Hour)) {
		t.Fatalf("next announcement must be two weeks later: %s", got)
	}
}
