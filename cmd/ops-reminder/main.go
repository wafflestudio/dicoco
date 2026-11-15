package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/wafflestudio/dicoco/internal/feature/opsreminder"
)

func main() {
	send := flag.Bool("send", false, "write verified Notion state and post eligible reminders; default is read-only dry run")
	testLimit := flag.Int("test-limit", 0, "analyze only this many recent candidates and label any post as a limited test (1-10)")
	mode := flag.String("mode", "daily", "run mode: daily, notion-sync, d1, or meeting-followup")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	cfg, err := opsreminder.LoadConfig(time.Now(), !*send)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ops-reminder:", err)
		os.Exit(1)
	}
	if *testLimit < 0 || *testLimit > 10 {
		fmt.Fprintln(os.Stderr, "-test-limit must be between 0 and 10")
		os.Exit(2)
	}
	cfg.TestLimit = *testLimit
	if (*mode == "daily" || *mode == "notion-sync") && *send && *testLimit > 0 {
		fmt.Fprintln(os.Stderr, "daily mode does not allow partial Notion writes with -test-limit")
		os.Exit(2)
	}
	if *mode != "daily" && *mode != "notion-sync" && *mode != "d1" && *mode != "meeting-followup" {
		fmt.Fprintln(os.Stderr, "-mode must be daily, notion-sync, d1, or meeting-followup")
		os.Exit(2)
	}
	cfg.Mode = *mode
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	result, err := opsreminder.Run(ctx, cfg)
	// Dry-run output includes task metadata, not raw source text or credentials.
	if !*send {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("source=%s target=%s collected=%d created_tasks=%d checked_tasks=%d eligible=%d already_sent=%d sent=%d\n", result.SourceMeetingDate, result.NextMeetingDate, result.Collected, result.CreatedTasks, result.CheckedTasks, result.Eligible, result.AlreadySent, result.Sent)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ops-reminder:", err)
		os.Exit(1)
	}
}
