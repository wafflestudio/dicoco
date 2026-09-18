package waffle

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	announcementIntervalDays  = 14
	announcementInterval      = announcementIntervalDays * 24 * time.Hour
	announcementWeekday       = time.Wednesday
	announcementHour          = 10 // Korea Standard Time (UTC+9).
	announcementCheckInterval = time.Minute
)

var announcementLocation = time.FixedZone("KST", 9*60*60)

func (h *Handler) Run(ctx context.Context) {
	h.checkAndAnnounce(ctx)

	ticker := time.NewTicker(announcementCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.checkAndAnnounce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (h *Handler) checkAndAnnounce(ctx context.Context) {
	if err := h.announceIfDue(ctx); err != nil {
		h.logger.Print("[waffle] 오류: 공지 처리 실패")
	}
}

func (h *Handler) announceIfDue(ctx context.Context) error {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()

	state, err := h.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if state.Version != stateVersion {
		return fmt.Errorf("unsupported state version %d", state.Version)
	}

	now := h.now().UTC()
	// Align legacy, deployment-relative schedules once; preserve valid schedules
	// across restarts, including overdue announcements.
	if state.NextAnnouncementAt == nil || !isScheduledTime(*state.NextAnnouncementAt) {
		nextAnnouncementAt := firstAnnouncementAt(now)
		state.NextAnnouncementAt = &nextAnnouncementAt
		if err := h.store.Save(ctx, state); err != nil {
			return fmt.Errorf("initialize next announcement time: %w", err)
		}
	}
	if now.Before(*state.NextAnnouncementAt) {
		return nil
	}

	announcementSent := len(state.Recipients) > 0
	if announcementSent {
		if h.sendMessage == nil {
			return fmt.Errorf("discord message sender is not registered")
		}
		if err := h.sendMessage(h.announcementChannelID, formatRanking(state.Recipients)); err != nil {
			return fmt.Errorf("send ranking: %w", err)
		}
	}

	state.Recipients = make(map[string]int)
	nextAnnouncementAt := nextAnnouncementAfter(*state.NextAnnouncementAt, now)
	state.NextAnnouncementAt = &nextAnnouncementAt
	if err := h.store.Save(ctx, state); err != nil {
		return fmt.Errorf("save next announcement state: %w", err)
	}

	if announcementSent {
		h.logger.Print("[waffle] 순위 공지 완료")
	}
	return nil
}

func formatRanking(recipients map[string]int) string {
	type entry struct {
		userID string
		count  int
	}

	entries := make([]entry, 0, len(recipients))
	for userID, count := range recipients {
		entries = append(entries, entry{userID: userID, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count == entries[j].count {
			return entries[i].userID < entries[j].userID
		}
		return entries[i].count > entries[j].count
	})

	medals := [...]string{"🥇", "🥈", "🥉"}
	limit := min(len(entries), len(medals))

	var message strings.Builder
	fmt.Fprint(&message, "🧇 이번 집계 와플 랭킹\n\n")
	for index := range limit {
		fmt.Fprintf(&message, "%s <@%s> — %d개", medals[index], entries[index].userID, entries[index].count)
		if index+1 < limit {
			message.WriteByte('\n')
		}
	}

	return message.String()
}

func nextAnnouncementAfter(next, now time.Time) time.Time {
	for !next.After(now) {
		next = next.Add(announcementInterval)
	}
	return next
}

func firstAnnouncementAt(now time.Time) time.Time {
	local := now.In(announcementLocation)
	days := (int(announcementWeekday) - int(local.Weekday()) + 7) % 7
	next := time.Date(local.Year(), local.Month(), local.Day()+days, announcementHour, 0, 0, 0, announcementLocation)
	if next.Before(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next.UTC()
}

func isScheduledTime(at time.Time) bool {
	local := at.In(announcementLocation)
	return local.Weekday() == announcementWeekday && local.Hour() == announcementHour &&
		local.Minute() == 0 && local.Second() == 0 && local.Nanosecond() == 0
}
