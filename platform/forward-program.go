// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	// From ossrs.
	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"

	// Use v8 because we use Go 1.16+, while v9 requires Go 1.18+
	"github.com/go-redis/redis/v8"
)

const (
	// The prefix of the internal program bus stream. User streams with this prefix are reserved
	// and hidden from the platform, they never appear in SRS_STREAM_ACTIVE.
	forwardProgramPrefix = "__forward_"

	// The program publisher mode.
	programModeIdle = "idle"
	programModeLive = "live"
	// The composite publisher renders the fallback slate itself (base + source overlay with
	// eof_action=pass), so a source outage never changes the publisher: no republish, and thus
	// no multi-second downstream gap (see runComposite).
	programModeComposite = "composite"

	// The failover mode: hold the last frame (default) or switch to a fallback slate.
	forwardFailoverHold  = "hold"
	forwardFailoverSlate = "slate"

	// The delay to wait after stopping a program publisher, to let SRS un-publish the stream
	// before the next publisher takes over.
	programHandoffDelay = 300 * time.Millisecond

	// The HTTP endpoint of the local SRS, used to read the source as HTTP-FLV.
	forwardProgramHTTPPort = 2022
	// The max reconnect delay when the source is temporarily offline.
	forwardReconnectDelayMax = 5
	// The HTTP status to reconnect on, when the stream is temporarily unavailable. The platform
	// proxies /live/*.flv to SRS, which may return 502 when a stream is not published yet, so
	// we must reconnect on both 4xx (404) and 5xx (502).
	forwardReconnectHTTPError = "4xx,5xx"

	// The no-update timeout for the forward fanout. It reads a program that holds the last
	// frame during a source outage, so it produces no frames for a while and must not be
	// killed (which would drop the downstream) as aggressively as a normal task.
	// Zero disables the no-update kill: the fanout must survive a program that is temporarily
	// unavailable or holding the last frame, otherwise the downstream is dropped for no reason.
	forwardFanoutNoUpdateTime = 0
)

// isForwardProgramStream reports whether the stream is an internal program bus stream, which
// should never be exposed to the platform or stored in Redis.
func isForwardProgramStream(_, stream string) bool {
	return strings.HasPrefix(stream, forwardProgramPrefix)
}

// forwardProgramStreamName builds the internal program stream name for a source stream.
func forwardProgramStreamName(app, stream string) string {
	return fmt.Sprintf("%v%v_%v", forwardProgramPrefix, app, stream)
}

// ProgramWorker manages all program buses. Each bus keeps an always-on program stream so that
// downstream forward tasks never see the source disappear, see docs/forward-program-bus.md.
type ProgramWorker struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup

	lock     sync.Mutex
	baseCtx  context.Context
	programs map[string]*ProgramBus
}

func NewProgramWorker() *ProgramWorker {
	return &ProgramWorker{programs: make(map[string]*ProgramBus)}
}

func (v *ProgramWorker) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	v.cancel = cancel

	ctx = logger.WithContext(ctx)
	v.baseCtx = ctx
	logger.Tf(ctx, "forward program start a worker")

	return nil
}

func (v *ProgramWorker) Close() error {
	if v.cancel != nil {
		v.cancel()
	}
	v.wg.Wait()
	return nil
}

// normalizeForwardFailover maps a failover value to a known mode, defaulting to hold.
func normalizeForwardFailover(failover string) string {
	if failover == forwardFailoverSlate {
		return forwardFailoverSlate
	}
	return forwardFailoverHold
}

// EnsureProgram returns the program bus for the source, creating and starting it on first use.
// The returned bus is reference counted, callers should call ReleaseProgram when done.
func (v *ProgramWorker) EnsureProgram(app, stream, failover string) *ProgramBus {
	v.lock.Lock()
	defer v.lock.Unlock()

	key := fmt.Sprintf("%v/%v", app, stream)
	if bus, ok := v.programs[key]; ok {
		bus.addRef()
		bus.setFailover(failover)
		return bus
	}

	if v.baseCtx == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(v.baseCtx)
	bus := &ProgramBus{
		sourceApp: app, sourceStream: stream,
		programApp: app, programStream: forwardProgramStreamName(app, stream),
		failover: normalizeForwardFailover(failover),
		cancel:   cancel,
		refs:     1,
	}
	v.programs[key] = bus

	v.wg.Add(1)
	go func() {
		defer v.wg.Done()
		bus.Run(ctx)
	}()

	return bus
}

// GetProgram returns the program bus for the source without changing the reference count, used
// only to query the status. Returns nil if there is no program for the source.
func (v *ProgramWorker) GetProgram(app, stream string) *ProgramBus {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.programs[fmt.Sprintf("%v/%v", app, stream)]
}

// ReleaseProgram drops a reference to the program bus, stopping it when the last one is gone.
func (v *ProgramWorker) ReleaseProgram(app, stream string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	key := fmt.Sprintf("%v/%v", app, stream)
	bus, ok := v.programs[key]
	if !ok {
		return
	}

	if bus.delRef() {
		bus.stop()
		delete(v.programs, key)
	}
}

// ProgramBus keeps an internal program stream always published, by running a persistent relay
// that reads the source via HTTP-FLV with reconnect. Because HTTP is the only protocol that
// FFmpeg can auto-reconnect, the relay (and thus the program publisher connection) survives a
// source outage: the program never un-publishes, so downstream consumers are never disconnected
// and simply hold the last frame until the source recovers, see docs/forward-program-bus.md.
type ProgramBus struct {
	sourceApp, sourceStream   string
	programApp, programStream string

	cancel context.CancelFunc

	lock         sync.Mutex
	refs         int
	mode         string
	failover     string
	pubCancel    context.CancelFunc
	pid          int32
	switches     int
	sourceActive bool
	// When the source first became inactive, to implement the optional offline-stop policy.
	inactiveSince time.Time
	lastSwitchAt  *time.Time
	lastError     string

	// Source reconnect evidence from the relay FFmpeg stderr.
	reconnects      int
	lastReconnectAt *time.Time
	reconnectReason string

	// The source's video size and frame rate, probed while it is live, so the slate can match
	// it — a resolution/fps change at the handoff would disturb the copied downstream.
	srcWidth, srcHeight int32
	// The source's average frame rate as a rational (e.g. "60" or "1475589000/24728941"), used
	// as the slate/base frame rate so the video's media clock tracks the source's real time.
	srcRate string

	// The program diagnostics timeline (bounded ring).
	events []ForwardEvent
}

// ProgramStatus is a snapshot of a program bus for the API.
type ProgramStatus struct {
	Source       string
	Program      string
	Mode         string
	Failover     string
	PID          int32
	Switches     int
	SourceActive bool
	// The source's probed average frame rate, to compare with the output (normalize/CFR).
	SourceRate   string
	LastSwitchAt string
	LastError    string

	Reconnects      int
	LastReconnectAt string
	ReconnectReason string

	Events []ForwardEvent
}

func (v *ProgramBus) String() string {
	v.lock.Lock()
	defer v.lock.Unlock()
	return fmt.Sprintf("source=%v/%v, program=%v/%v, mode=%v, pid=%v, switches=%v, source_active=%v",
		v.sourceApp, v.sourceStream, v.programApp, v.programStream, v.mode, v.pid, v.switches, v.sourceActive)
}

func (v *ProgramBus) addRef() {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.refs++
}

func (v *ProgramBus) delRef() bool {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.refs--
	return v.refs <= 0
}

func (v *ProgramBus) stop() {
	if v.cancel != nil {
		v.cancel()
	}
}

func (v *ProgramBus) setFailover(failover string) {
	if failover != forwardFailoverSlate {
		return
	}
	v.lock.Lock()
	defer v.lock.Unlock()
	v.failover = forwardFailoverSlate
}

func (v *ProgramBus) failoverMode() string {
	v.lock.Lock()
	defer v.lock.Unlock()
	if v.failover == "" {
		return forwardFailoverHold
	}
	return v.failover
}

func (v *ProgramBus) setPID(pid int32) {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.pid = pid
}

func (v *ProgramBus) setSourceActive(active bool) {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.sourceActive = active
	if active {
		v.inactiveSince = time.Time{}
	} else if v.inactiveSince.IsZero() {
		v.inactiveSince = time.Now()
	}
}

// sourceInactiveFor reports how long the source has been inactive (zero when active or unknown).
func (v *ProgramBus) sourceInactiveFor() time.Duration {
	v.lock.Lock()
	defer v.lock.Unlock()
	if v.sourceActive || v.inactiveSince.IsZero() {
		return 0
	}
	return time.Since(v.inactiveSince)
}

func (v *ProgramBus) setLastError(err error) {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.lastError = err.Error()
}

// onRelayLog records the source reconnect evidence from the relay FFmpeg stderr, for example:
//
//	[http @ ...] Will reconnect at 408673 in 0 second(s), error=End of file.
//	[http @ ...] HTTP error 502 Bad Gateway
//
// Each "Will reconnect" is one reconnect attempt against the source. The buffered stderr line
// may contain a long progress blob, so the reason is trimmed to the [http @ ...] marker.
func (v *ProgramBus) onRelayLog(line string) {
	if !strings.Contains(line, "Will reconnect") && !strings.Contains(line, "HTTP error") {
		return
	}

	reason := strings.TrimSpace(line)
	if i := strings.Index(reason, "[http @"); i >= 0 {
		reason = reason[i:]
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}

	v.lock.Lock()
	defer v.lock.Unlock()

	if strings.Contains(line, "Will reconnect") {
		v.reconnects++
		v.addEventLocked("program-reconnect", reason)
	}
	now := time.Now()
	v.lastReconnectAt = &now
	v.reconnectReason = reason
}

func (v *ProgramBus) addEventLocked(name, detail string) {
	v.events = append(v.events, ForwardEvent{At: time.Now().Format(time.RFC3339), Name: name, Detail: detail})
	if len(v.events) > forwardDiagRingSize {
		v.events = v.events[len(v.events)-forwardDiagRingSize:]
	}
}

func (v *ProgramBus) queryStatus() ProgramStatus {
	v.lock.Lock()
	defer v.lock.Unlock()

	status := ProgramStatus{
		Source:          fmt.Sprintf("%v/%v", v.sourceApp, v.sourceStream),
		Program:         fmt.Sprintf("%v/%v", v.programApp, v.programStream),
		Mode:            v.mode,
		Failover:        v.failover,
		PID:             v.pid,
		Switches:        v.switches,
		SourceActive:    v.sourceActive,
		SourceRate:      v.srcRate,
		LastError:       v.lastError,
		Reconnects:      v.reconnects,
		ReconnectReason: v.reconnectReason,
	}
	if status.Mode == "" {
		status.Mode = programModeIdle
	}
	if status.Failover == "" {
		status.Failover = forwardFailoverHold
	}
	if v.lastSwitchAt != nil {
		status.LastSwitchAt = v.lastSwitchAt.Format(time.RFC3339)
	}
	if v.lastReconnectAt != nil {
		status.LastReconnectAt = v.lastReconnectAt.Format(time.RFC3339)
	}
	status.Events = append([]ForwardEvent(nil), v.events...)
	return status
}

func (v *ProgramBus) isSourceActive(ctx context.Context) bool {
	streamURL := fmt.Sprintf("%v/%v", v.sourceApp, v.sourceStream)
	if _, err := rdb.HGet(ctx, SRS_STREAM_ACTIVE, streamURL).Result(); err != nil && err != redis.Nil {
		logger.Wf(ctx, "ignore program source check err %+v", err)
		return false
	} else if err == redis.Nil {
		return false
	}
	return true
}

// Run keeps the program published. The failover mode decides how a source outage is handled.
func (v *ProgramBus) Run(ctx context.Context) {
	ctx = logger.WithContext(ctx)
	logger.Tf(ctx, "forward program bus run, %v", v.String())

	if v.failoverMode() == forwardFailoverSlate {
		v.runComposite(ctx)
	} else {
		v.runHold(ctx)
	}
}

// runHold runs the live relay persistently. It reads the source via HTTP-FLV with reconnect, so
// it survives a source outage and keeps the program published; the downstream stays connected
// and holds the last frame until the source recovers.
func (v *ProgramBus) runHold(ctx context.Context) {
	if err := v.switchPublisher(ctx, programModeLive); err != nil {
		logger.Wf(ctx, "forward program start relay err %+v", err)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	wasActive := true
	for {
		active := v.isSourceActive(ctx)
		v.setSourceActive(active)

		// FFmpeg's HTTP-FLV reconnect does not reliably re-publish the program output after the
		// source returns (the program stays un-published and the fanout never resumes), so
		// restart the relay on recovery (false->true) for a clean re-publish. The fanout reads
		// the program via HTTP-FLV with reconnect, so it survives this publisher change.
		if active && !wasActive {
			logger.Tf(ctx, "forward program source recovered, restart relay")
			if err := v.switchPublisher(ctx, programModeLive); err != nil {
				logger.Wf(ctx, "forward program restart relay err %+v", err)
			}
		}
		wasActive = active

		select {
		case <-ctx.Done():
			v.stopPublisher()
			logger.Tf(ctx, "forward program bus done, %v", v.String())
			return
		case <-ticker.C:
		}
	}
}

// runComposite runs a single persistent publisher that renders the picture itself: a base (dark
// colour + clock) with the source overlaid, end-on-EOF. When the source goes away the overlay
// passes the base, so the program keeps flowing with NO publisher change — unlike switching to a
// separate slate publisher, which republishes the program and was the cause of the multi-second
// downstream gap. The fanout therefore never sees a republish, and the outage is seamless. On
// recovery the composite is restarted to read the source again (a source that ended cannot be
// re-added to a running filtergraph), which is the only remaining (brief) reconnect.
func (v *ProgramBus) runComposite(ctx context.Context) {
	// Learn the source size/fps while it is live, so the slate base matches it.
	v.probeSourceSize(ctx)
	if err := v.switchPublisher(ctx, programModeComposite); err != nil {
		logger.Wf(ctx, "forward program start composite err %+v", err)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	wasActive := true
	for {
		active := v.isSourceActive(ctx)
		v.setSourceActive(active)

		if active && !wasActive {
			logger.Tf(ctx, "forward program source recovered, restart composite")
			v.probeSourceSize(ctx)
			if err := v.switchPublisher(ctx, programModeComposite); err != nil {
				logger.Wf(ctx, "forward program restart composite err %+v", err)
			}
		}
		wasActive = active

		select {
		case <-ctx.Done():
			v.stopPublisher()
			logger.Tf(ctx, "forward program bus done, %v", v.String())
			return
		case <-ticker.C:
		}
	}
}

func (v *ProgramBus) stopPublisher() {
	v.lock.Lock()
	cancel := v.pubCancel
	v.pubCancel = nil
	v.pid = 0
	v.mode = programModeIdle
	v.lock.Unlock()

	if cancel != nil {
		cancel()
		time.Sleep(programHandoffDelay)
	}
}

func (v *ProgramBus) switchPublisher(parent context.Context, mode string) error {
	v.stopPublisher()

	ctx, cancel := context.WithCancel(parent)

	v.lock.Lock()
	v.mode = mode
	v.pubCancel = cancel
	v.switches++
	now := time.Now()
	v.lastSwitchAt = &now
	switches := v.switches
	v.addEventLocked("program-switch", mode)
	v.lock.Unlock()

	logger.Tf(parent, "forward program switch, program=%v/%v, mode=%v, switches=%v",
		v.programApp, v.programStream, mode, switches)

	go func() {
		defer cancel()
		for ctx.Err() == nil {
			if err := v.runPublisherOnce(ctx, mode); err != nil {
				v.setLastError(err)
				logger.Wf(parent, "forward program publisher err %+v, retry in 2s", err)

				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
			return
		}
	}()

	return nil
}

func (v *ProgramBus) runPublisherOnce(ctx context.Context, mode string) error {
	output := v.programPublishURL(ctx)

	args := []string{}
	switch mode {
	case programModeLive:
		// Read the source via HTTP-FLV with reconnect. HTTP is the only protocol FFmpeg can
		// auto-reconnect, so when the source is temporarily offline the relay stays alive (and
		// keeps the program publisher slot) instead of exiting, which would un-publish the
		// program and disconnect every downstream consumer.
		srcURL := fmt.Sprintf("http://localhost:%v/%v/%v.flv", forwardProgramHTTPPort, v.sourceApp, v.sourceStream)
		args = append(args,
			"-reconnect", "1", "-reconnect_at_eof", "1", "-reconnect_streamed", "1",
			"-reconnect_on_http_error", forwardReconnectHTTPError,
			"-reconnect_delay_max", fmt.Sprintf("%v", forwardReconnectDelayMax),
			"-i", srcURL,
		)
		// With the global normalize option on, the program is transcoded ONCE here so every
		// downstream target can -c copy the identical stream. Otherwise it is a zero-transcode
		// relay. (Experiment C2 showed the republish, not the codec change, is the handoff root
		// cause.)
		conf := forwardSlateConfigLoad(ctx)
		if conf.Normalize {
			_, _, rate := v.sourceSize()
			gop := forwardGOPFrames(rate, conf.KeyframeSeconds)
			args = append(args, "-fflags", "+genpts", "-fps_mode", "cfr")
			args = append(args, forwardVideoEncodeArgs(conf.OutputBitrateMbps, conf.OutputCQ, conf.OutputPresets)...)
			args = append(args, "-g", fmt.Sprintf("%d", gop), "-keyint_min", fmt.Sprintf("%d", gop),
				"-sc_threshold", "0", "-c:a", "aac", "-b:a", "128k", "-ar", "44100")
		} else {
			args = append(args, "-c", "copy")
		}
		args = append(args, "-f", "flv", output)
	case programModeComposite:
		// A single persistent publisher that renders the slate itself: a dark base with a
		// ticking clock, the source overlaid end-on-EOF, and the audio mixed with silence (so
		// it lasts when the source ends). The program never republishes, so a source outage is
		// seamless for the downstream, see runComposite.
		w, h, rate := v.sourceSize()
		srcURL := fmt.Sprintf("http://localhost:%v/%v/%v.flv", forwardProgramHTTPPort, v.sourceApp, v.sourceStream)
		slateConf := forwardSlateConfigLoad(ctx)
		filter := fmt.Sprintf(
			"[0:v]%v,%v[bg];[bg][2:v]overlay=eof_action=pass:x=0:y=0[v];[1:a][2:a]amix=inputs=2:duration=longest:dropout_transition=0[a]",
			forwardSlateTextDrawtext(forwardSlateActiveText(slateConf, forwardEndedAny(ctx))), forwardSlateDrawtext(),
		)
		args = append(args,
			"-re",
			"-f", "lavfi", "-i", forwardSlateVideo(w, h, rate),
			"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=44100",
			"-i", srcURL,
			"-filter_complex", filter,
			"-map", "[v]", "-map", "[a]",
		)
		gop := forwardGOPFrames(rate, slateConf.KeyframeSeconds)
		args = append(args, forwardVideoEncodeArgs(slateConf.OutputBitrateMbps, slateConf.OutputCQ, slateConf.OutputPresets)...)
		args = append(args, "-g", fmt.Sprintf("%d", gop), "-keyint_min", fmt.Sprintf("%d", gop),
			"-sc_threshold", "0", "-c:a", "aac", "-f", "flv", output)
	default:
		return errors.Errorf("invalid program mode %v", mode)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	// The slate clock uses the process timezone; honor the configured one (the container default
	// is usually UTC, which would show the wrong local time).
	if mode == programModeComposite {
		if conf := forwardSlateConfigLoad(ctx); conf.TZ != "" {
			cmd.Env = append(os.Environ(), "TZ="+conf.TZ)
		}
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.Wrapf(err, "pipe process")
	}

	if err := cmd.Start(); err != nil {
		return errors.Wrapf(err, "execute ffmpeg %v", strings.Join(args, " "))
	}

	v.setPID(int32(cmd.Process.Pid))
	defer v.setPID(0)
	logger.Tf(ctx, "forward program publisher started, mode=%v, pid=%v", mode, cmd.Process.Pid)

	// Drain the stderr (otherwise FFmpeg blocks when the pipe buffer is full) and count the
	// reconnect attempts, the source-side evidence of a flaky upstream.
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			v.onRelayLog(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			logger.Wf(context.Background(), "forward relay stderr scan err %+v", err)
		}
	}()

	err = cmd.Wait()
	if ctx.Err() != nil {
		// Stopped by the program bus on purpose.
		return nil
	}

	return err
}

// programPublishURL builds the internal publish URL for the program stream, including the
// global publish secret (if any) so the SRS on_publish hook accepts it. Note that program
// streams are hidden from Redis by the hook, see isForwardProgramStream.
func (v *ProgramBus) programPublishURL(ctx context.Context) string {
	url := fmt.Sprintf("rtmp://localhost/%v/%v", v.programApp, v.programStream)
	if secret := forwardPublishSecret(ctx); secret != "" {
		url += "?secret=" + secret
	}
	return url
}

// forwardPublishSecret returns the global publish secret used to authenticate the internal
// program publisher against the SRS on_publish hook.
func forwardPublishSecret(ctx context.Context) string {
	if s, err := rdb.HGet(ctx, SRS_AUTH_SECRET, "pubSecret").Result(); err != nil && err != redis.Nil {
		logger.Wf(ctx, "ignore hget pubSecret err %+v", err)
	} else {
		return s
	}
	return ""
}

// forwardSlateVideo is the lavfi video source for the slate (a dark solid color), sized and paced
// to match the source so switching does not change the resolution or frame rate mid-stream. The
// rate is the source's average frame rate, so the video's media clock tracks the source's real
// time (a source that reports 60 but averages 59.67 would otherwise drift ~33s/hour against its
// own audio, because the audio keeps the source timestamps while this base re-clocks the video).
func forwardSlateVideo(w, h int32, rate string) string {
	if w <= 0 || h <= 0 {
		w, h = 1280, 720
	}
	if rate == "" {
		rate = "25"
	}
	return fmt.Sprintf("color=c=0x111111:s=%dx%d:r=%v", w, h, rate)
}

// forwardSlateRate picks the base frame rate from ffprobe's average/r frame rate, preferring the
// average (the real media rate) and reducing it to a rational ffmpeg accepts.
func forwardSlateRate(rFrame, avgFrame string) string {
	reduce := func(s string) (string, bool) {
		num, den, ok := parseFPS(s)
		if !ok || den <= 0 || num <= 0 {
			return "", false
		}
		if g := gcd(num, den); g > 1 {
			num, den = num/g, den/g
		}
		if num/den < 1 || num/den > 120 {
			return "", false
		}
		if den == 1 {
			return fmt.Sprintf("%d", num), true
		}
		return fmt.Sprintf("%d/%d", num, den), true
	}
	if s, ok := reduce(avgFrame); ok {
		return s
	}
	if s, ok := reduce(rFrame); ok {
		return s
	}
	return "25"
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		a = -a
	}
	return a
}

// setSourceSize / sourceSize guard the probed source size and frame rate.
func (v *ProgramBus) setSourceSize(w, h int32, rate string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.srcWidth, v.srcHeight, v.srcRate = w, h, rate
}

func (v *ProgramBus) sourceSize() (w, h int32, rate string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.srcWidth, v.srcHeight, v.srcRate
}

// probeSourceSize probes the live source once to learn its video size and frame rate, so the
// slate can match it and avoid a resolution/fps change at the handoff.
func (v *ProgramBus) probeSourceSize(ctx context.Context) {
	url := fmt.Sprintf("http://localhost:%v/%v/%v.flv", forwardProgramHTTPPort, v.sourceApp, v.sourceStream)
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,r_frame_rate,avg_frame_rate", "-of", "csv=p=0", url).Output()
	if err != nil {
		return
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 4 {
		return
	}
	w, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	rate := forwardSlateRate(parts[2], parts[3])
	// Bound the dimensions before narrowing to int32: ffprobe output is untrusted, and a huge
	// value would overflow the conversion (and no real video exceeds 16K).
	const maxVideoDim = 16384
	if w > 0 && h > 0 && w <= maxVideoDim && h <= maxVideoDim {
		v.setSourceSize(int32(w), int32(h), rate)
		logger.Tf(ctx, "forward program probed source size %vx%v@%v (r=%v avg=%v)", w, h, rate, parts[2], parts[3])
	}
}

// parseFPS parses an ffprobe frame rate such as "25/1" or "30000/1001".
func parseFPS(s string) (num, den int, ok bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i > 0 {
		n, err1 := strconv.Atoi(strings.TrimSpace(s[:i]))
		d, err2 := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		return n, d, err1 == nil && err2 == nil && d > 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, 1, true
	}
	return 0, 0, false
}

// forwardSlateDrawtext overlays a realtime clock on the slate, so viewers can tell the stream is
// still alive. It degrades to a pass-through when no font is available.
func forwardSlateDrawtext() string {
	font := forwardSlateFont()
	if font == "" {
		return "null"
	}
	return fmt.Sprintf("drawtext=fontfile=%v:text='%%{localtime\\:%%Y-%%m-%%d %%T}':fontcolor=white:fontsize=48:x=(w-text_w)/2:y=(h-text_h)/2", font)
}

// forwardSlateFont picks an available font for the slate clock, or "" if none is found.
func forwardSlateFont() string {
	for _, f := range []string{
		// Prefer a CJK-capable font first: Roboto has no Chinese glyphs, so the slate text
		// (e.g. the outage message) would render as tofu boxes.
		"/usr/local/share/fonts/NotoSansSC-Regular.ttf",
		"/usr/local/share/fonts/Roboto-Regular.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/System/Library/Fonts/Supplemental/Arial.ttf",
	} {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}

// forwardSlateActiveText picks the message for the composite overlay: the end text once the
// operator has ended the live, otherwise the source-outage text.
func forwardSlateActiveText(conf forwardSlateConfig, ended bool) string {
	if ended && conf.EndText != "" {
		return conf.EndText
	}
	return conf.Text
}

// forwardEndedAny reports whether any target is in the manual "ended" state, so the shared
// program shows the end text. Per-target text would require a separate program per target.
func forwardEndedAny(ctx context.Context) bool {
	if rdb == nil {
		return false
	}
	items, err := rdb.HGetAll(ctx, SRS_FORWARD_CONFIG).Result()
	if err != nil {
		return false
	}
	for _, raw := range items {
		var c ForwardConfigure
		if json.Unmarshal([]byte(raw), &c) == nil && c.Ended {
			return true
		}
	}
	return false
}
