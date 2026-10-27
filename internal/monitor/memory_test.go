package monitor

import (
	"os"
	"testing"
)

func TestDetectCgroupVersions(t *testing.T) {
	for _, version := range []string{"cgroup_v2", "cgroup_v1"} {
		t.Run(version, func(t *testing.T) {
			path, source, err := detect(func(path string) ([]byte, error) {
				if (version == "cgroup_v2" && path == "/sys/fs/cgroup/memory.current") || (version == "cgroup_v1" && path == "/sys/fs/cgroup/memory/memory.usage_in_bytes") {
					return []byte("838860800\n"), nil
				}
				return nil, os.ErrNotExist
			})
			if err != nil || path == "" || source != version {
				t.Fatalf("got %q %q %v", path, source, err)
			}
		})
	}
}

func TestInvalidUsage(t *testing.T) {
	for _, data := range []string{"max", "-1", ""} {
		if _, err := parse([]byte(data)); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	if _, _, err := detect(func(string) ([]byte, error) { return nil, os.ErrNotExist }); err == nil {
		t.Fatal("must not substitute Go heap for container memory")
	}
}
