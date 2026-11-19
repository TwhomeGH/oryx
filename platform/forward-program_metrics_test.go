package main

import (
	"strings"
	"testing"
)

// The progress line is the only source for the program's transcode cost, so the parse must handle
// the exact strings FFmpeg emits (including "1.01x" and "N/A").
func TestParseProgramProgressBlock(t *testing.T) {
	block := `frame=1250
fps=25.02
stream_0_0_q=28.0
bitrate=4580.2kbits/s
total_size=1048576
out_time_us=50000000
out_time_ms=50000000
out_time=00:00:50.000000
dup_frames=0
drop_frames=0
speed=1.01x
progress=continue
`
	var pm ProgramMetrics
	for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		applyProgramProgress(&pm, key, value)
	}
	if pm.Frame != 1250 {
		t.Fatalf("frame = %d", pm.Frame)
	}
	if pm.FPS != 25.02 || pm.Speed != 1.01 || pm.BitrateKbps != 4580.2 {
		t.Fatalf("fps/speed/bitrate = %v/%v/%v", pm.FPS, pm.Speed, pm.BitrateKbps)
	}
	if pm.MediaSeconds != 50 {
		t.Fatalf("media seconds = %v", pm.MediaSeconds)
	}
}

// "N/A" appears when FFmpeg has no rate yet; it must not poison the numbers.
func TestApplyProgramProgressIgnoresInvalid(t *testing.T) {
	var pm ProgramMetrics
	applyProgramProgress(&pm, "speed", "N/A")
	applyProgramProgress(&pm, "bitrate", "N/A")
	applyProgramProgress(&pm, "fps", "")
	if pm.Speed != 0 || pm.BitrateKbps != 0 || pm.FPS != 0 {
		t.Fatalf("invalid values leaked: %+v", pm)
	}
}
