// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// From ossrs.
	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"

	// Use v8 because we use Go 1.16+, while v9 requires Go 1.18+
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var forwardWorker *ForwardWorker

const (
	forwardStateIdle        = "idle"
	forwardStateWaiting     = "waiting_input"
	forwardStateRunning     = "running"
	forwardStateRetrying    = "retrying"
	forwardStateDegraded    = "degraded"
	forwardMaxRetryDelay    = 60 * time.Second
	forwardBaseRetryDelay   = 3500 * time.Millisecond
	forwardCircuitThreshold = 5
)

type ForwardWorker struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// The tasks we have started to forward streams,, key is platform in string, value is *ForwardTask.
	tasks sync.Map
}

func NewForwardWorker() *ForwardWorker {
	return &ForwardWorker{}
}

func (v *ForwardWorker) GetTask(platform string) *ForwardTask {
	if task, loaded := v.tasks.Load(platform); loaded {
		return task.(*ForwardTask)
	}
	return nil
}

// RemoveTask stops the running FFmpeg process of the task if exists, then removes it
// from the worker, so that it is never reported by streams API again. Note that the
// Run loop goroutine keeps running till worker closed, but stays idle because its
// config is disabled.
func (v *ForwardWorker) RemoveTask(platform string) {
	if task := v.GetTask(platform); task != nil {
		task.lock.Lock()
		task.config.Enabled = false
		if task.cancel != nil {
			task.cancel()
		}
		task.lock.Unlock()
	}

	v.tasks.Delete(platform)
}

func (v *ForwardWorker) Handle(ctx context.Context, handler *http.ServeMux) error {
	ep := "/terraform/v1/ffmpeg/forward/secret"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var action string
			var userConf ForwardConfigure
			if err := ParseBody(ctx, r.Body, &struct {
				Action *string `json:"action"`
				*ForwardConfigure
			}{
				Action: &action, ForwardConfigure: &userConf,
			}); err != nil {
				return errors.Wrapf(err, "parse body")
			}

			allowedActions := []string{"update", "delete"}
			allowedPlatforms := []string{"wx", "bilibili", "kuaishou"}
			if action != "" {
				if !slicesContains(allowedActions, action) {
					return errors.Errorf("invalid action=%v", action)
				}

				if userConf.Platform == "" {
					return errors.New("no platform")
				}

				// Platform should be specified platforms, or starts with forwarding-.
				if !slicesContains(allowedPlatforms, userConf.Platform) && !strings.Contains(userConf.Platform, "forwarding-") {
					return errors.Errorf("invalid platform=%v", userConf.Platform)
				}

				// Only update requires the target server; delete works by platform only.
				if action == "update" && userConf.Server == "" {
					return errors.New("no server")
				}
			}

			switch action {
			case "update":
				// Allow pasting a full RTMP(S) ingest URL into the Server field, split it
				// into server and secret automatically.
				userConf.Server, userConf.Secret = NormalizeRTMPConfig(userConf.Server, userConf.Secret)

				var targetConf ForwardConfigure
				if config, err := rdb.HGet(ctx, SRS_FORWARD_CONFIG, userConf.Platform).Result(); err != nil && err != redis.Nil {
					return errors.Wrapf(err, "hget %v %v", SRS_FORWARD_CONFIG, userConf.Platform)
				} else {
					isNewConfig := config == ""
					if isNewConfig && isForwardCustomPlatform(userConf.Platform, userConf.Customed) {
						if err := ensureForwardCustomLimit(ctx); err != nil {
							return err
						}
					}
					if config != "" {
						if err = json.Unmarshal([]byte(config), &targetConf); err != nil {
							return errors.Wrapf(err, "unmarshal %v", config)
						}
					}
					if err = targetConf.Update(&userConf); err != nil {
						return errors.Wrapf(err, "update %v with %v", targetConf.String(), userConf.String())
					} else if newB, err := json.Marshal(&targetConf); err != nil {
						return errors.Wrapf(err, "marshal %v", targetConf.String())
					} else if err = rdb.HSet(ctx, SRS_FORWARD_CONFIG, userConf.Platform, string(newB)).Err(); err != nil && err != redis.Nil {
						return errors.Wrapf(err, "hset %v %v %v", SRS_FORWARD_CONFIG, userConf.Platform, string(newB))
					}
				}

				// Restart the forwarding if exists.
				if task := v.GetTask(userConf.Platform); task != nil {
					if err := task.Restart(ctx); err != nil {
						return errors.Wrapf(err, "restart task %v", userConf.String())
					}
				}

				ohttp.WriteData(ctx, w, r, nil)
				logger.Tf(ctx, "Forward update secret ok")
				return nil
			case "delete":
				// Only custom forwarding configs are removable, built-in platforms are protected.
				if !strings.Contains(userConf.Platform, "forwarding-") {
					return errors.Errorf("invalid platform=%v", userConf.Platform)
				}

				if err := rdb.HDel(ctx, SRS_FORWARD_CONFIG, userConf.Platform).Err(); err != nil && err != redis.Nil {
					return errors.Wrapf(err, "hdel %v %v", SRS_FORWARD_CONFIG, userConf.Platform)
				}
				v.RemoveTask(userConf.Platform)

				ohttp.WriteData(ctx, w, r, nil)
				logger.Tf(ctx, "forward delete config ok, platform=%v", userConf.Platform)
				return nil
			default:
				confObjs := make(map[string]*ForwardConfigure)
				if configs, err := rdb.HGetAll(ctx, SRS_FORWARD_CONFIG).Result(); err != nil && err != redis.Nil {
					return errors.Wrapf(err, "hgetall %v", SRS_FORWARD_CONFIG)
				} else {
					for k, v := range configs {
						var obj ForwardConfigure
						if err = json.Unmarshal([]byte(v), &obj); err != nil {
							return errors.Wrapf(err, "unmarshal %v %v", k, v)
						}
						confObjs[k] = &obj
					}
				}

				ohttp.WriteData(ctx, w, r, confObjs)
				logger.Tf(ctx, "forward query configures ok")
				return nil
			}
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	ep = "/terraform/v1/ffmpeg/forward/streams"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			res := make([]map[string]interface{}, 0)
			if configItems, err := rdb.HGetAll(ctx, SRS_FORWARD_CONFIG).Result(); err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hgetall %v", SRS_FORWARD_CONFIG)
			} else if len(configItems) > 0 {
				for k, configItem := range configItems {
					var config ForwardConfigure
					if err = json.Unmarshal([]byte(configItem), &config); err != nil {
						return errors.Wrapf(err, "unmarshal %v %v", k, configItem)
					}

					status := ForwardTaskStatus{State: forwardStateIdle}
					if task := v.GetTask(config.Platform); task != nil {
						status = task.queryStatus()
					}

					elem := map[string]interface{}{
						"platform": config.Platform,
						"enabled":  config.Enabled,
						"custom":   config.Customed,
						"label":    config.Label,
						"status":   status.State,
					}
					if status.Input != "" {
						elem["stream"] = status.Input
					}
					if status.Start != "" {
						elem["start"] = status.Start
					}
					if status.Ready != "" {
						elem["ready"] = status.Ready
					}
					if status.LastError != "" {
						elem["lastError"] = status.LastError
						elem["lastErrorAt"] = status.LastErrorAt
					}
					if status.NextRetryAt != "" {
						elem["nextRetryAt"] = status.NextRetryAt
					}
					elem["restartCount"] = status.RestartCount
					elem["consecutiveFailures"] = status.ConsecutiveFailures

					if status.PID > 0 {
						elem["frame"] = map[string]string{
							"log":    status.Frame,
							"update": status.FrameUpdate,
						}
					}

					res = append(res, elem)
				}
			}

			sort.Slice(res, func(i, j int) bool {
				return res[i]["platform"].(string) < res[j]["platform"].(string)
			})

			ohttp.WriteData(ctx, w, r, res)
			logger.Tf(ctx, "Query forward streams ok")
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	return nil
}

func (v *ForwardWorker) Close() error {
	if v.cancel != nil {
		v.cancel()
	}
	v.wg.Wait()
	return nil
}

func (v *ForwardWorker) Start(ctx context.Context) error {
	wg := &v.wg

	ctx, cancel := context.WithCancel(ctx)
	v.cancel = cancel

	ctx = logger.WithContext(ctx)
	logger.Tf(ctx, "forward start a worker")

	// Load tasks from redis and force to kill all.
	if objs, err := rdb.HGetAll(ctx, SRS_FORWARD_TASK).Result(); err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hgetall %v", SRS_FORWARD_TASK)
	} else if len(objs) > 0 {
		for uuid, obj := range objs {
			logger.Tf(ctx, "Load task %v object %v", uuid, obj)

			var task ForwardTask
			if err = json.Unmarshal([]byte(obj), &task); err != nil {
				return errors.Wrapf(err, "unmarshal %v %v", uuid, obj)
			}

			if task.PID > 0 {
				task.cleanup(ctx)
			}
		}

		if err = rdb.Del(ctx, SRS_FORWARD_TASK).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "del %v", SRS_FORWARD_TASK)
		}
	}

	// Load all configurations from redis.
	loadTasks := func() error {
		configItems, err := rdb.HGetAll(ctx, SRS_FORWARD_CONFIG).Result()
		if err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hgetall %v", SRS_FORWARD_CONFIG)
		}
		if len(configItems) == 0 {
			return nil
		}

		for platform, configItem := range configItems {
			var config ForwardConfigure
			if err = json.Unmarshal([]byte(configItem), &config); err != nil {
				return errors.Wrapf(err, "unmarshal %v %v", platform, configItem)
			}

			var task *ForwardTask
			if tv, loaded := v.tasks.LoadOrStore(config.Platform, &ForwardTask{
				UUID:     uuid.NewString(),
				Platform: config.Platform,
				config:   &config,
			}); loaded {
				// Ignore if exists.
				continue
			} else {
				task = tv.(*ForwardTask)
				logger.Tf(ctx, "Forward create platform=%v task is %v", platform, task.String())
			}

			// Initialize object.
			if err := task.Initialize(ctx, v); err != nil {
				return errors.Wrapf(err, "init %v", task.String())
			}

			// Store in memory object.
			v.tasks.Store(platform, task)

			wg.Add(1)
			go func() {
				defer wg.Done()

				if err := task.Run(ctx); err != nil {
					logger.Wf(ctx, "run task %v err %+v", task.String(), err)
				}
			}()
		}

		return nil
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		// When startup, we try to wait for client to publish streams.
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
		logger.Tf(ctx, "forward start to run tasks")

		for ctx.Err() == nil {
			duration := 3 * time.Second
			if err := loadTasks(); err != nil {
				logger.Wf(ctx, "ignore err %+v", err)
				duration = 10 * time.Second
			}

			select {
			case <-ctx.Done():
			case <-time.After(duration):
			}
		}
	}()

	return nil
}

// ForwardConfigure is the configure for forwarding.
type ForwardConfigure struct {
	// The platform name, for example, wx
	Platform string `json:"platform"`
	// the source stream in oryx.
	Stream string `json:"stream"`
	// The RTMP server url, for example, rtmp://localhost/live
	Server string `json:"server"`
	// The RTMP stream and secret, for example, livestream
	Secret string `json:"secret"`
	// Whether enabled.
	Enabled bool `json:"enabled"`
	// Whether custom platform.
	Customed bool `json:"custom"`
	// The label for this configure.
	Label string `json:"label"`
}

func (v *ForwardConfigure) String() string {
	return fmt.Sprintf("platform=%v, stream=%v, server=%v, secret=%v, enabled=%v, customed=%v, label=%v",
		v.Platform, v.Stream, v.Server, v.Secret, v.Enabled, v.Customed, v.Label,
	)
}

func (v *ForwardConfigure) Update(u *ForwardConfigure) error {
	v.Platform = u.Platform
	v.Stream = u.Stream
	v.Server = u.Server
	v.Secret = u.Secret
	v.Label = u.Label
	v.Enabled = u.Enabled
	v.Customed = u.Customed
	return nil
}

// ForwardTask is a task for FFmpeg to forward stream, with a configure.
type ForwardTask struct {
	// The ID for task.
	UUID string `json:"uuid"`
	// The platform for task.
	Platform string `json:"platform"`

	// The input url.
	Input string `json:"input"`
	// The input stream URL.
	inputStreamURL string
	// The output url
	Output string `json:"output"`

	// FFmpeg pid.
	PID int32 `json:"pid"`
	// FFmpeg last frame.
	frame string
	// The last update time.
	update *time.Time
	// The task start time.
	starttime *time.Time
	// The first ready time.
	firstReadyTime *time.Time
	// Runtime health state for API observability and retry control.
	State               string     `json:"state,omitempty"`
	RestartCount        int        `json:"restartCount,omitempty"`
	ConsecutiveFailures int        `json:"consecutiveFailures,omitempty"`
	LastError           string     `json:"lastError,omitempty"`
	LastErrorAt         *time.Time `json:"lastErrorAt,omitempty"`
	NextRetryAt         *time.Time `json:"nextRetryAt,omitempty"`

	// The context for current task.
	cancel context.CancelFunc

	// The configure for forwarding task.
	config *ForwardConfigure
	// The forward worker.
	forwardWorker *ForwardWorker

	// To protect the fields.
	lock sync.Mutex
}

type ForwardTaskStatus struct {
	PID                 int32
	State               string
	Input               string
	Frame               string
	FrameUpdate         string
	Start               string
	Ready               string
	LastError           string
	LastErrorAt         string
	NextRetryAt         string
	RestartCount        int
	ConsecutiveFailures int
}

func (v *ForwardTask) String() string {
	return fmt.Sprintf("uuid=%v, platform=%v, input=%v, output=%v, pid=%v, frame=%vB, config is %v",
		v.UUID, v.Platform, v.Input, v.Output, v.PID, len(v.frame), v.config.String(),
	)
}

func (v *ForwardTask) saveTask(ctx context.Context) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	if b, err := json.Marshal(v); err != nil {
		return errors.Wrapf(err, "marshal %v", v.String())
	} else if err = rdb.HSet(ctx, SRS_FORWARD_TASK, v.UUID, string(b)).Err(); err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hset %v %v %v", SRS_FORWARD_TASK, v.UUID, string(b))
	}

	return nil
}

func (v *ForwardTask) cleanup(ctx context.Context) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	if v.PID <= 0 {
		return nil
	}

	logger.Wf(ctx, "kill task pid=%v", v.PID)
	syscall.Kill(int(v.PID), syscall.SIGKILL)

	v.PID = 0
	v.cancel = nil

	return nil
}

func (v *ForwardTask) Restart(ctx context.Context) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	if v.cancel != nil {
		v.cancel()
	}
	v.State = forwardStateRetrying
	v.NextRetryAt = nil
	v.LastError = ""
	v.LastErrorAt = nil
	v.ConsecutiveFailures = 0

	// Reload config from redis.
	if b, err := rdb.HGet(ctx, SRS_FORWARD_CONFIG, v.Platform).Result(); err != nil {
		return errors.Wrapf(err, "hget %v %v", SRS_FORWARD_CONFIG, v.Platform)
	} else if err = json.Unmarshal([]byte(b), v.config); err != nil {
		return errors.Wrapf(err, "unmarshal %v", b)
	}

	return nil
}

func (v *ForwardTask) updateFrame(frame string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.frame = strings.TrimSpace(frame)

	var now = time.Now()
	v.update = &now
}

func (v *ForwardTask) queryStatus() ForwardTaskStatus {
	v.lock.Lock()
	defer v.lock.Unlock()

	status := ForwardTaskStatus{
		PID:                 v.PID,
		State:               v.State,
		Input:               v.inputStreamURL,
		Frame:               strings.TrimSpace(v.frame),
		RestartCount:        v.RestartCount,
		ConsecutiveFailures: v.ConsecutiveFailures,
		LastError:           v.LastError,
	}
	if status.State == "" {
		status.State = forwardStateIdle
	}
	if v.firstReadyTime != nil {
		status.Ready = v.firstReadyTime.Format(time.RFC3339)
	}
	if v.update != nil {
		status.FrameUpdate = v.update.Format(time.RFC3339)
	}
	if v.starttime != nil {
		status.Start = v.starttime.Format(time.RFC3339)
	}
	if v.LastErrorAt != nil {
		status.LastErrorAt = v.LastErrorAt.Format(time.RFC3339)
	}
	if v.NextRetryAt != nil {
		status.NextRetryAt = v.NextRetryAt.Format(time.RFC3339)
	}
	return status
}

func (v *ForwardTask) markWaitingInput() {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.State = forwardStateWaiting
	v.NextRetryAt = nil
}

func (v *ForwardTask) markRunning(pid int32, inputURL, inputStreamURL, outputURL string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.PID = pid
	v.Input, v.inputStreamURL, v.Output = inputURL, inputStreamURL, outputURL
	v.State = forwardStateRunning
	v.NextRetryAt = nil
}

func (v *ForwardTask) markFailure(err error) time.Duration {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.ConsecutiveFailures++
	v.RestartCount++
	now := time.Now()
	v.LastError = err.Error()
	v.LastErrorAt = &now
	delay := forwardRetryDelay(v.ConsecutiveFailures)
	next := now.Add(delay)
	v.NextRetryAt = &next
	if v.ConsecutiveFailures >= forwardCircuitThreshold {
		v.State = forwardStateDegraded
	} else {
		v.State = forwardStateRetrying
	}
	return delay
}

func (v *ForwardTask) markHealthy() {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.ConsecutiveFailures = 0
	v.LastError = ""
	v.LastErrorAt = nil
	v.NextRetryAt = nil
	if v.config != nil && v.config.Enabled {
		v.State = forwardStateWaiting
	} else {
		v.State = forwardStateIdle
	}
}

func forwardRetryDelay(failures int) time.Duration {
	if failures <= 1 {
		return forwardBaseRetryDelay
	}

	multiplier := math.Pow(2, float64(failures-1))
	delay := time.Duration(float64(forwardBaseRetryDelay) * multiplier)
	if delay > forwardMaxRetryDelay {
		return forwardMaxRetryDelay
	}
	return delay
}

func ensureForwardCustomLimit(ctx context.Context) error {
	limit := 10
	if envForwardLimit() != "" {
		if iv, err := strconv.Atoi(envForwardLimit()); err != nil {
			return errors.Wrapf(err, "parse env forward limit %v", envForwardLimit())
		} else {
			limit = iv
		}
	}
	if limit <= 0 {
		return errors.Errorf("invalid forward limit %v", limit)
	}

	configItems, err := rdb.HGetAll(ctx, SRS_FORWARD_CONFIG).Result()
	if err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hgetall %v", SRS_FORWARD_CONFIG)
	}

	customs := 0
	for platform, configItem := range configItems {
		var config ForwardConfigure
		if err = json.Unmarshal([]byte(configItem), &config); err != nil {
			return errors.Wrapf(err, "unmarshal %v %v", platform, configItem)
		}
		if isForwardCustomPlatform(config.Platform, config.Customed) {
			customs++
		}
	}
	if customs >= limit {
		return errors.Errorf("forward custom config limit exceeded, limit=%v", limit)
	}

	return nil
}

func isForwardCustomPlatform(platform string, customed bool) bool {
	return customed || strings.Contains(platform, "forwarding-")
}

func (v *ForwardTask) Initialize(ctx context.Context, w *ForwardWorker) error {
	v.forwardWorker = w
	logger.Tf(ctx, "forward initialize uuid=%v, platform=%v", v.UUID, v.Platform)

	if err := v.saveTask(ctx); err != nil {
		return errors.Wrapf(err, "save task")
	}

	return nil
}

func (v *ForwardTask) Run(ctx context.Context) error {
	ctx = logger.WithContext(ctx)
	logger.Tf(ctx, "forward run task %v", v.String())

	// select active stream by stream name or random select one when stream name is empty.
	selectActiveStream := func() (*SrsStream, error) {
		streams, err := rdb.HGetAll(ctx, SRS_STREAM_ACTIVE).Result()
		if err != nil {
			return nil, errors.Wrapf(err, "hgetall %v", SRS_STREAM_ACTIVE)
		}

		streamName := v.config.Stream

		var best *SrsStream
		for _, v := range streams {
			var stream SrsStream
			if err := json.Unmarshal([]byte(v), &stream); err != nil {
				return nil, errors.Wrapf(err, "unmarshal %v", v)
			}
			if streamName != "" {
				if stream.Stream == streamName {
					best = &stream
					break
				}
				continue
			}

			if best == nil {
				best = &stream
				continue
			}

			bestUpdate, err := time.Parse(time.RFC3339, best.Update)
			if err != nil {
				return nil, errors.Wrapf(err, "parse %v", best.Update)
			}

			streamUpdate, err := time.Parse(time.RFC3339, stream.Update)
			if err != nil {
				return nil, errors.Wrapf(err, "parse %v", stream.Update)
			}

			if bestUpdate.Before(streamUpdate) {
				best = &stream
			}
		}

		// Ignore if no active stream.
		if best == nil {
			return nil, nil
		}

		logger.Tf(ctx, "forward use best=%v as input for platform=%v", best.StreamURL(), v.Platform)
		return best, nil
	}

	pfn := func(ctx context.Context) error {
		// Ignore when not enabled.
		if !v.config.Enabled {
			v.markHealthy()
			return nil
		}

		// Use a active stream as input.
		input, err := selectActiveStream()
		if err != nil {
			return errors.Wrapf(err, "select input")
		}

		if input == nil {
			v.markWaitingInput()
			return nil
		}

		// Start forward task.
		if err := v.doForward(ctx, input); err != nil {
			return errors.Wrapf(err, "do forward")
		}

		return nil
	}

	for ctx.Err() == nil {
		if err := pfn(ctx); err != nil {
			delay := v.markFailure(err)
			logger.Wf(ctx, "ignore %v err %+v, retry in %v", v.String(), err, delay)

			select {
			case <-ctx.Done():
			case <-time.After(delay):
			}
			continue
		}
		v.markHealthy()

		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}

	return nil
}

func (v *ForwardTask) doForward(ctx context.Context, input *SrsStream) error {
	// Create context for current task.
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	v.cancel = cancel

	// Build input URL.
	host := "localhost"
	inputURL := fmt.Sprintf("rtmp://%v/%v/%v", host, input.App, input.Stream)

	// Build output URL.
	outputServer := strings.ReplaceAll(v.config.Server, "localhost", host)
	if !strings.HasSuffix(outputServer, "/") && !strings.HasPrefix(v.config.Secret, "/") && v.config.Secret != "" {
		outputServer += "/"
	}
	outputURL := fmt.Sprintf("%v%v", outputServer, v.config.Secret)

	// B4: Check the output URL structure before probing, to fail fast with a clear reason
	// instead of a cryptic FFmpeg error like exit status 251.
	if err := CheckRTMPOutputURL(outputURL); err != nil {
		return errors.Wrapf(err, "output url precheck")
	}

	// B3: Probe the output server before starting FFmpeg, to fail fast with a clear reason,
	// instead of a cryptic FFmpeg dial error after several seconds.
	if err := ProbeTCPServer(ctx, outputURL, 2*time.Second); err != nil {
		return errors.Wrapf(err, "output precheck")
	}

	// Create a heartbeat to poll and manage the status of FFmpeg process.
	heartbeat := NewFFmpegHeartbeat(cancel)
	v.starttime, v.firstReadyTime = &heartbeat.starttime, nil
	defer func() {
		v.starttime = nil
	}()

	// Start FFmpeg process.
	args := []string{}
	args = append(args, "-re")
	// For RTSP stream source, always use TCP transport.
	if strings.HasPrefix(inputURL, "rtsp://") {
		args = append(args, "-rtsp_transport", "tcp")
	}
	// Rebuild the stream url, because it may contain special characters.
	if strings.Contains(inputURL, "://") {
		if u, err := RebuildStreamURL(inputURL); err != nil {
			return errors.Wrapf(err, "rebuild %v", inputURL)
		} else {
			args = append(args, "-i", u.String())
			heartbeat.Parse(u)
		}
	} else {
		args = append(args, "-i", inputURL)
	}
	args = append(args, "-c", "copy")
	// If RTMP use flv, if SRT use mpegts, otherwise do not set.
	if strings.HasPrefix(outputURL, "rtmp://") || strings.HasPrefix(outputURL, "rtmps://") {
		args = append(args, "-f", "flv")
	} else if strings.HasPrefix(outputURL, "srt://") {
		args = append(args, "-pes_payload_size", "0", "-f", "mpegts")
	}
	args = append(args, outputURL)
	// Create the command object.
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.Wrapf(err, "pipe process")
	}

	if err := cmd.Start(); err != nil {
		return errors.Wrapf(err, "execute ffmpeg %v", strings.Join(args, " "))
	}

	v.markRunning(int32(cmd.Process.Pid), inputURL, input.StreamURL(), outputURL)
	defer func() {
		// If we got a PID, sleep for a while, to avoid too fast restart.
		if v.PID > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(1 * time.Second):
			}
		}

		// When canceled, we should still write to redis, so we must not use ctx(which is cancelled).
		v.cleanup(parentCtx)
		v.saveTask(parentCtx)
	}()
	logger.Tf(ctx, "forward start, platform=%v, stream=%v, pid=%v", v.Platform, input.StreamURL(), v.PID)

	if err := v.saveTask(ctx); err != nil {
		return errors.Wrapf(err, "save task %v", v.String())
	}

	inputGone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := rdb.HGet(ctx, SRS_STREAM_ACTIVE, input.StreamURL()).Result(); err == redis.Nil {
					logger.Tf(ctx, "forward input stream gone, platform=%v, stream=%v", v.Platform, input.StreamURL())
					close(inputGone)
					cancel()
					return
				} else if err != nil {
					logger.Wf(ctx, "ignore forward input stream check err %+v", err)
				}
			}
		}
	}()

	// Pull the latest log frame.
	heartbeat.Polling(ctx, stderr)
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.firstReadyCtx.Done():
			v.firstReadyTime = &heartbeat.firstReadyTime
		}

		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-heartbeat.FrameLogs:
				v.updateFrame(frame)
			}
		}
	}()

	// Process terminated, or user cancel the process.
	select {
	case <-parentCtx.Done():
	case <-ctx.Done():
	case <-inputGone:
	case <-heartbeat.PollingCtx.Done():
	}
	logger.Tf(ctx, "Forward: Cycle stopping, platform=%v, stream=%v, pid=%v",
		v.Platform, input.StreamURL(), v.PID)

	err = cmd.Wait()
	logger.Tf(ctx, "forward done, platform=%v, stream=%v, pid=%v, err=%v",
		v.Platform, input.StreamURL(), v.PID, err,
	)

	select {
	case <-inputGone:
		return nil
	default:
	}

	return err
}
