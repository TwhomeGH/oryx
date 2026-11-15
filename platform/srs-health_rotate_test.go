package main

import (
	"context"
	"os"
	"testing"
)

// SRS never rotates its own log (verified: a 31MB pre-filled file is simply appended to), so the
// platform must cap it. This exercises the real file path with a small fixture.
func TestRotateSrsLogIfNeeded(t *testing.T) {
	savedPath, savedCap, savedKeep := srsLogPath, srsLogCapBytes, srsLogKeepBytes
	srsLogPath = t.TempDir() + "/srs.log"
	srsLogCapBytes, srsLogKeepBytes = 1024, 256
	defer func() {
		srsLogPath, srsLogCapBytes, srsLogKeepBytes = savedPath, savedCap, savedKeep
	}()

	if _, err := rotateSrsLogIfNeeded(context.Background()); err != nil {
		t.Fatalf("missing file must be a no-op, got %v", err)
	}

	// Under the cap: untouched.
	small := make([]byte, 512)
	for i := range small {
		small[i] = 'a'
	}
	if err := os.WriteFile(srsLogPath, small, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := rotateSrsLogIfNeeded(context.Background()); err != nil {
		t.Fatalf("rotate under cap err %v", err)
	}
	if st, _ := os.Stat(srsLogPath); st.Size() != 512 {
		t.Fatalf("file under cap changed size to %d", st.Size())
	}

	// Over the cap: trimmed to the newest keep bytes, and the tail must survive verbatim.
	big := make([]byte, 4096)
	for i := range big {
		big[i] = 'x'
	}
	copy(big[len(big)-8:], "TAIL-END")
	if err := os.WriteFile(srsLogPath, big, 0640); err != nil {
		t.Fatal(err)
	}
	kept, err := rotateSrsLogIfNeeded(context.Background())
	if err != nil {
		t.Fatalf("rotate over cap err %v", err)
	}
	if kept != 256 {
		t.Fatalf("kept = %d, want 256", kept)
	}
	got, err := os.ReadFile(srsLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 256 {
		t.Fatalf("file size = %d, want 256", len(got))
	}
	if string(got[len(got)-8:]) != "TAIL-END" {
		t.Fatalf("tail lost, ends with %q", string(got[len(got)-8:]))
	}
}
