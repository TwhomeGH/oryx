package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// SRS health cannot come from the HTTP API: /api/v1/summaries reports live resource usage only and
// carries no error counters, while the reasons that actually matter (publish timeout, kickoff,
// queue overflow, corrupt packets) exist only in SRS's own log. Since the container now writes that
// log to a file (and mirrors it to stdout), this card reads the tail and groups the messages.
const (
	// srsLogPath is where the container writes SRS's own log; tests override srsLogPath.
	srsLogPathDefault = "/data/logs/srs.log"
	// Only the tail is scanned: a long-running container accumulates a large file and the card
	// only needs the recent picture.
	srsHealthWindowBytes = 256 * 1024
	srsHealthMaxGroups   = 6
	srsHealthMaxRecent   = 12
)

// srsLogPath is a variable so unit tests can point it at a fixture file.
var srsLogPath = srsLogPathDefault

// SRS appends to its log file and never rotates it: measured by pre-filling the file to 31MB and
// restarting SRS, which kept the file and appended (fd flags are O_RDWR|O_APPEND, so truncating is
// safe and cannot leave a sparse hole). Cap it here so a long-running container cannot fill the
// data volume: at 16MB keep the newest 4MB. Variables, not constants, so tests can shrink them.
var (
	srsLogCapBytes  int64 = 16 * 1024 * 1024
	srsLogKeepBytes int64 = 4 * 1024 * 1024
)

// srsLogLine is one parsed SRS log line.
type srsLogLine struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// srsErrorGroup is a normalized message with a count.
type srsErrorGroup struct {
	Pattern string `json:"pattern"`
	Level   string `json:"level"`
	Count   int    `json:"count"`
	LastAt  string `json:"lastAt"`
	Sample  string `json:"sample"`
}

// srsHealthResult is what the console card renders.
type srsHealthResult struct {
	LogPath      string          `json:"logPath"`
	LogBytes     int64           `json:"logBytes"`
	WindowBytes  int             `json:"windowBytes"`
	Lines        int             `json:"lines"`
	ErrorCount   int             `json:"errorCount"`
	WarnCount    int             `json:"warnCount"`
	Groups       []srsErrorGroup `json:"groups"`
	RecentErrors []srsLogLine    `json:"recentErrors"`
	// Targeted counters for the failures the card calls out explicitly.
	// CapBytes is the rotation threshold, so the card can show it is bounded on purpose.
	CapBytes       int64 `json:"capBytes"`
	PublishTimeout int   `json:"publishTimeout"`
	Kickoff        int   `json:"kickoff"`
	Overflow       int   `json:"overflow"`
	TimestampIssue int   `json:"timestampIssue"`
}

var srsLogLinePattern = regexp.MustCompile(`^\[([^\]]+)\]\[([A-Z]+)\]\[[^\]]*\]\[[^\]]*\]\s*(.*)$`)

// Normalizing a message is what makes grouping useful: SRS embeds ids, ports, sizes and hex
// pointers in the text, so without stripping them every occurrence looks unique.
var (
	srsLogHexPattern     = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	srsLogNumberPattern  = regexp.MustCompile(`\b\d+\b`)
	srsLogContextPattern = regexp.MustCompile(`^\[\d+\]\s*`)
)

// rotateSrsLogIfNeeded caps the SRS log. It runs before every read and from a slow ticker, so the
// file stays bounded even when nobody opens the console card.
func rotateSrsLogIfNeeded(ctx context.Context) (int64, error) {
	stat, err := os.Stat(srsLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, errors.Wrapf(err, "stat %v", srsLogPath)
	}
	if stat.Size() <= srsLogCapBytes {
		return 0, nil
	}

	f, err := os.Open(srsLogPath)
	if err != nil {
		return 0, errors.Wrapf(err, "open %v", srsLogPath)
	}
	tail := make([]byte, srsLogKeepBytes)
	offset := stat.Size() - srsLogKeepBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return 0, errors.Wrapf(err, "seek %v", srsLogPath)
	}
	n, err := io.ReadFull(f, tail)
	f.Close()
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return 0, errors.Wrapf(err, "read %v", srsLogPath)
	}
	tail = tail[:n]

	// Truncate-and-write: SRS appends with O_APPEND, so it writes to the new end of file and the
	// bytes we keep stay at offset 0.
	w, err := os.OpenFile(srsLogPath, os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return 0, errors.Wrapf(err, "truncate %v", srsLogPath)
	}
	if _, err := w.Write(tail); err != nil {
		w.Close()
		return 0, errors.Wrapf(err, "rewrite %v", srsLogPath)
	}
	if err := w.Close(); err != nil {
		return 0, errors.Wrapf(err, "close %v", srsLogPath)
	}

	kept := int64(len(tail))
	logger.Tf(ctx, "srs log rotated, was=%vB now=%vB", stat.Size(), kept)
	return kept, nil
}

// querySrsHealth reads the SRS log tail and summarizes it.
func querySrsHealth(ctx context.Context) (*srsHealthResult, error) {
	if _, err := rotateSrsLogIfNeeded(ctx); err != nil {
		logger.Wf(ctx, "rotate srs log err %v", err)
	}

	f, err := os.Open(srsLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &srsHealthResult{LogPath: srsLogPath}, nil
		}
		return nil, errors.Wrapf(err, "open %v", srsLogPath)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, errors.Wrapf(err, "stat %v", srsLogPath)
	}

	offset := stat.Size() - srsHealthWindowBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, errors.Wrapf(err, "seek %v", srsLogPath)
	}

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrapf(err, "read %v", srsLogPath)
	}

	return parseSrsHealthLog(raw, stat.Size()), nil
}

// parseSrsHealthLog is the pure part of the query: it turns the log tail into counters and groups
// so the behaviour can be tested without a real SRS log on disk.
func parseSrsHealthLog(raw []byte, totalBytes int64) *srsHealthResult {
	res := &srsHealthResult{LogPath: srsLogPath, LogBytes: totalBytes, WindowBytes: len(raw), CapBytes: srsLogCapBytes}
	groups := map[string]*srsErrorGroup{}
	for _, line := range strings.Split(string(raw), "\n") {
		m := srsLogLinePattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		res.Lines++
		ts, level, msg := m[1], m[2], m[3]

		// Some SRS lines carry a leading context id such as "[2] " inside the message; strip it so
		// grouping and the sample text describe the message itself.
		msg = srsLogContextPattern.ReplaceAllString(msg, "")
		normalized := srsLogNumberPattern.ReplaceAllString(srsLogHexPattern.ReplaceAllString(msg, "0x?"), "N")

		switch {
		case strings.Contains(msg, "publish timeout"):
			res.PublishTimeout++
		case strings.Contains(msg, "kickoff"):
			res.Kickoff++
		case strings.Contains(msg, "overflow"):
			res.Overflow++
		}
		if strings.Contains(msg, "Non-monotonous DTS") || strings.Contains(msg, "non monotonically") || strings.Contains(msg, "Invalid timestamps") {
			res.TimestampIssue++
		}

		if level != "ERROR" && level != "WARN" {
			continue
		}
		if level == "ERROR" {
			res.ErrorCount++
			if len(res.RecentErrors) < srsHealthMaxRecent {
				res.RecentErrors = append(res.RecentErrors, srsLogLine{Time: ts, Level: level, Message: msg})
			}
		} else {
			res.WarnCount++
		}

		g := groups[normalized]
		if g == nil {
			g = &srsErrorGroup{Pattern: normalized, Level: level, Sample: msg}
			groups[normalized] = g
		}
		g.Count++
		g.LastAt = ts
	}

	for _, g := range groups {
		res.Groups = append(res.Groups, *g)
	}
	sort.Slice(res.Groups, func(i, j int) bool {
		if res.Groups[i].Count != res.Groups[j].Count {
			return res.Groups[i].Count > res.Groups[j].Count
		}
		return res.Groups[i].LastAt > res.Groups[j].LastAt
	})
	if len(res.Groups) > srsHealthMaxGroups {
		res.Groups = res.Groups[:srsHealthMaxGroups]
	}

	return res
}

// handleMgmtSrsHealthQuery serves the 診斷 card.
func handleMgmtSrsHealthQuery(ctx context.Context, handler *http.ServeMux) error {
	ep := "/terraform/v1/mgmt/srs/health/query"
	logger.Tf(ctx, "Handle %v", ep)

	// Cap the log even when nobody opens the card: a 10-minute ticker is enough for a 16MB cap.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := rotateSrsLogIfNeeded(ctx); err != nil {
					logger.Wf(ctx, "rotate srs log err %v", err)
				}
			}
		}
	}()

	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			res, err := querySrsHealth(ctx)
			if err != nil {
				return errors.Wrapf(err, "query srs health")
			}
			ohttp.WriteData(ctx, w, r, res)
			logger.Tf(ctx, "srs health ok, bytes=%v errors=%v", res.LogBytes, res.ErrorCount)
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	return nil
}
