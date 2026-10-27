// Package monitor observes the entire Linux container, including OpenCode and
// its children. Go's heap counters alone would miss most of that memory.
package monitor

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	Interval       = 10 * time.Second
	ReportInterval = time.Minute
	Threshold      = uint64(800 * 1024 * 1024)
)

func Run(ctx context.Context) {
	path, source, err := detect(os.ReadFile)
	if err != nil {
		log.Printf("[memory] container monitoring unavailable: %v", err)
		return
	}
	log.Printf("[memory] sampling every 10s, reporting every 1m; warning at 800MiB; source=%s (includes page cache)", source)
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	var nextReport time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[memory] read usage: %v", err)
				continue
			}
			usage, err := parse(data)
			if err != nil {
				log.Printf("[memory] parse usage: %v", err)
				continue
			}
			if usage >= Threshold {
				log.Printf("[memory] HIGH container_memory=%.1fMiB threshold=800MiB source=%s", float64(usage)/(1024*1024), source)
			} else if !now.Before(nextReport) {
				log.Printf("[memory] container_memory=%.1fMiB source=%s", float64(usage)/(1024*1024), source)
			}
			if !now.Before(nextReport) {
				nextReport = now.Add(ReportInterval)
			}
		}
	}
}

func detect(readFile func(string) ([]byte, error)) (string, string, error) {
	for _, candidate := range []struct{ path, source string }{
		{"/sys/fs/cgroup/memory.current", "cgroup_v2"},
		{"/sys/fs/cgroup/memory/memory.usage_in_bytes", "cgroup_v1"},
	} {
		data, err := readFile(candidate.path)
		if err != nil {
			continue
		}
		if _, err := parse(data); err != nil {
			continue
		}
		return candidate.path, candidate.source, nil
	}
	return "", "", fmt.Errorf("no readable container cgroup memory counter (monitor is for Linux containers)")
}

func parse(data []byte) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}
