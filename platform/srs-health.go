package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// SRS health cannot come from the HTTP API: /api/v1/summaries reports live resource usage only and
// carries no error counters, while the reasons that actually matter (publish timeout, kickoff,
// queue overflow, corrupt packets) exist only in SRS's own log. Since the container now writes that
// log to a file (and mirrors it to stdout), this card reads the tail and groups the messages.
const (
	srsHealthLogPath = "/data/logs/srs.log"
	// Only the tail is scanned: a long-running container accumulates a large file and the card
	// only needs the recent picture.
	srsHealthWindowBytes = 256 * 1024
	srsHealthMaxGroups   = 6
	srsHealthMaxRecent   = 12
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
	PublishTimeout int `json:"publishTimeout"`
	Kickoff        int `json:"kickoff"`
	Overflow       int `json:"overflow"`
	TimestampIssue int `json:"timestampIssue"`
}

var srsLogLinePattern = regexp.MustCompile(`^\[([^\]]+)\]\[([A-Z]+)\]\[[^\]]*\]\[[^\]]*\]\s*(.*)$`)

// Normalizing a message is what makes grouping useful: SRS embeds ids, ports, sizes and hex
// pointers in the text, so without stripping them every occurrence looks unique.
var (
	srsLogHexPattern     = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	srsLogNumberPattern  = regexp.MustCompile(`\b\d+\b`)
	srsLogContextPattern = regexp.MustCompile(`^\[\d+\]\s*`)
)

// querySrsHealth reads the SRS log tail and summarizes it.
func querySrsHealth(ctx context.Context) (*srsHealthResult, error) {
	f, err := os.Open(srsHealthLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &srsHealthResult{LogPath: srsHealthLogPath}, nil
		}
		return nil, errors.Wrapf(err, "open %v", srsHealthLogPath)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, errors.Wrapf(err, "stat %v", srsHealthLogPath)
	}

	offset := stat.Size() - srsHealthWindowBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, errors.Wrapf(err, "seek %v", srsHealthLogPath)
	}

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrapf(err, "read %v", srsHealthLogPath)
	}

	return parseSrsHealthLog(raw, stat.Size()), nil
}

// parseSrsHealthLog is the pure part of the query: it turns the log tail into counters and groups
// so the behaviour can be tested without a real SRS log on disk.
func parseSrsHealthLog(raw []byte, totalBytes int64) *srsHealthResult {
	res := &srsHealthResult{LogPath: srsHealthLogPath, LogBytes: totalBytes, WindowBytes: len(raw)}
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
