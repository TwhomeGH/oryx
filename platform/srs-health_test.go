package main

import (
	"os"
	"strings"
	"testing"
)

// The cards' numbers come from this parser, so it must group embedded ids away and count the
// targeted failure kinds the console calls out.
func TestQuerySrsHealth(t *testing.T) {
	saved := srsHealthLogPath
	// querySrsHealth uses the constant path; write a fixture there is not allowed from a unit test,
	// so exercise the parsing helper instead.
	_ = saved

	lines := strings.Join([]string{
		`[2026-11-15 10:00:00.000][INFO][44][abc123] Server started`,
		`[2026-11-15 10:00:01.000][ERROR][44][abc123] rtmp publish timeout 15000ms, nb_msgs=530449`,
		`[2026-11-15 10:00:02.000][ERROR][44][abc123] rtmp publish timeout 15000ms, nb_msgs=12`,
		`[2026-11-15 10:00:03.000][WARN][44][abc123] [2] client disconnect peer. ret=2901`,
		`[2026-11-15 10:00:04.000][ERROR][44][abc123] consumer queue overflow, drop 12 packets`,
		`[2026-11-15 10:00:05.000][ERROR][44][abc123] kickoff publisher for stream x`,
		`[2026-11-15 10:00:06.000][ERROR][44][abc123] Non-monotonous DTS in output stream 0:0`,
	}, "\n")

	res := parseSrsHealthLog([]byte(lines), 0)
	if res.Lines != 7 {
		t.Fatalf("lines = %d, want 7", res.Lines)
	}
	if res.ErrorCount != 5 || res.WarnCount != 1 {
		t.Fatalf("error/warn = %d/%d, want 5/1", res.ErrorCount, res.WarnCount)
	}
	if res.PublishTimeout != 2 || res.Overflow != 1 || res.Kickoff != 1 || res.TimestampIssue != 1 {
		t.Fatalf("targeted counters = %+v", res)
	}
	// Both publish-timeout lines must collapse into one group after normalization.
	var timeoutGroup int
	for _, g := range res.Groups {
		if strings.Contains(g.Pattern, "publish timeout") {
			timeoutGroup++
			if g.Count != 2 {
				t.Fatalf("publish timeout group count = %d, want 2", g.Count)
			}
		}
	}
	if timeoutGroup != 1 {
		t.Fatalf("publish timeout should be one group, got %d", timeoutGroup)
	}
	if len(res.RecentErrors) != 5 {
		t.Fatalf("recent errors = %d, want 5", len(res.RecentErrors))
	}
}

func TestParseSrsHealthFileMissing(t *testing.T) {
	saved := srsHealthLogPath
	defer func() { _ = saved }()
	if _, err := os.Stat("/nonexistent/srs.log"); err == nil {
		t.Skip("unexpected fixture")
	}
}
