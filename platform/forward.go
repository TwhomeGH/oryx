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

	// Whether to restart the fanout when the program publisher changes. Experiment C (see
	// docs) showed that NOT restarting leaves the fanout stuck with no output frames after the
	// program is republished, so the restart is required; the multi-second gap remains until
	// the program publisher itself can change without republishing (design B).
	forwardFanoutRestartOnSwitch = true
)

type ForwardWorker struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// The program worker keeps the internal program streams alive, so that downstream
	// forwarding is not interrupted when a source stream goes offline, see
	// docs/forward-program-bus.md.
	programWorker *ProgramWorker

	// The tasks we have started to forward streams,, key is platform in string, value is *ForwardTask.
	tasks sync.Map
}

func NewForwardWorker() *ForwardWorker {
	return &ForwardWorker{programWorker: NewProgramWorker()}
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

// forwardMasterEnabled reports the global forward switch. When it is off, every forward is
// paused regardless of each target's own enabled preference, so "stop all" does not discard the
// user's chosen set of platforms.
func forwardMasterEnabled(ctx context.Context) bool {
	if v, err := rdb.Get(ctx, SRS_FORWARD_ENABLED).Result(); err != nil && err != redis.Nil {
		logger.Wf(ctx, "ignore forward master get err %+v", err)
	} else if v == "false" {
		return false
	}
	return true
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

			allowedActions := []string{"update", "delete", "toggle"}
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

				// Validate the failover mode, empty means the default hold-last-frame.
				if userConf.Failover != "" && userConf.Failover != forwardFailoverHold &&
					userConf.Failover != forwardFailoverSlate {
					return errors.Errorf("invalid failover=%v", userConf.Failover)
				}

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
			case "toggle":
				// Enable/disable one target without resending its (possibly secret) full config.
				if userConf.Platform == "" {
					return errors.Errorf("invalid platform=%v", userConf.Platform)
				}
				var toggleConf ForwardConfigure
				if config, err := rdb.HGet(ctx, SRS_FORWARD_CONFIG, userConf.Platform).Result(); err != nil && err != redis.Nil {
					return errors.Wrapf(err, "hget %v %v", SRS_FORWARD_CONFIG, userConf.Platform)
				} else if config == "" {
					return errors.Errorf("no such platform=%v", userConf.Platform)
				} else if err = json.Unmarshal([]byte(config), &toggleConf); err != nil {
					return errors.Wrapf(err, "unmarshal %v", config)
				}
				toggleConf.Enabled = userConf.Enabled
				if newB, err := json.Marshal(&toggleConf); err != nil {
					return errors.Wrapf(err, "marshal %v", toggleConf.String())
				} else if err = rdb.HSet(ctx, SRS_FORWARD_CONFIG, userConf.Platform, string(newB)).Err(); err != nil && err != redis.Nil {
					return errors.Wrapf(err, "hset %v %v %v", SRS_FORWARD_CONFIG, userConf.Platform, string(newB))
				}
				if task := v.GetTask(userConf.Platform); task != nil {
					if err := task.Restart(ctx); err != nil {
						return errors.Wrapf(err, "restart task %v", userConf.Platform)
					}
				}
				ohttp.WriteData(ctx, w, r, nil)
				logger.Tf(ctx, "forward toggle ok, platform=%v enabled=%v", userConf.Platform, userConf.Enabled)
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

	// The global forward switch: query or toggle whether all forwarding is paused, without
	// changing each target's own enabled preference.
	ep = "/terraform/v1/ffmpeg/forward/enabled"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var action string
			enabled := true
			if err := ParseBody(ctx, r.Body, &struct {
				Action  *string `json:"action"`
				Enabled *bool   `json:"enabled"`
			}{Action: &action, Enabled: &enabled}); err != nil {
				return errors.Wrapf(err, "parse body")
			}

			switch action {
			case "", "query":
				ohttp.WriteData(ctx, w, r, &struct {
					Enabled bool `json:"enabled"`
				}{Enabled: forwardMasterEnabled(ctx)})
			case "update":
				if err := rdb.Set(ctx, SRS_FORWARD_ENABLED, fmt.Sprintf("%v", enabled), 0).Err(); err != nil && err != redis.Nil {
					return errors.Wrapf(err, "set %v", SRS_FORWARD_ENABLED)
				}
				// Apply immediately: a task only re-checks the switch between FFmpeg runs, so a
				// running forward must be restarted to pause it (otherwise the pause appears to
				// do nothing until the current run ends).
				v.tasks.Range(func(_, tv interface{}) bool {
					if task, ok := tv.(*ForwardTask); ok {
						if err := task.Restart(ctx); err != nil {
							logger.Wf(ctx, "forward restart for master switch err %+v", err)
						}
					}
					return true
				})
				ohttp.WriteData(ctx, w, r, &struct {
					Enabled bool `json:"enabled"`
				}{Enabled: enabled})
				logger.Tf(ctx, "forward master switch enabled=%v", enabled)
			default:
				return errors.Errorf("invalid action=%v", action)
			}
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	// The fallback slate configuration: timezone of the clock, the explanatory text, and whether
	// to stop a forward after the source has been offline for a while.
	ep = "/terraform/v1/ffmpeg/forward/slate"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var action string
			conf := defaultForwardSlateConfig()
			if err := ParseBody(ctx, r.Body, &struct {
				Action *string `json:"action"`
				*forwardSlateConfig
			}{Action: &action, forwardSlateConfig: &conf}); err != nil {
				return errors.Wrapf(err, "parse body")
			}

			switch action {
			case "", "query":
				ohttp.WriteData(ctx, w, r, forwardSlateConfigLoad(ctx))
			case "update":
				if err := forwardSlateConfigSave(ctx, conf); err != nil {
					return err
				}
				ohttp.WriteData(ctx, w, r, forwardSlateConfigLoad(ctx))
			default:
				return errors.Errorf("invalid action=%v", action)
			}
			return nil
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

					task := v.GetTask(config.Platform)
					status := ForwardTaskStatus{State: forwardStateIdle}
					if task != nil {
						status = task.queryStatus()
					}

					elem := map[string]interface{}{
						"platform":  config.Platform,
						"enabled":   config.Enabled,
						"custom":    config.Customed,
						"label":     config.Label,
						"status":    status.State,
						"normalize": config.Normalize,
					}
					if status.Input != "" {
						elem["stream"] = status.Input
						// The local output stream name (safe, no secret), so the console can offer a
						// direct preview of the forwarded picture for local targets.
						if task != nil {
							if own := task.ownOutputStream(); own != "" {
								elem["ownStream"] = own
							}
						}
					}
					if status.Program != "" {
						elem["program"] = status.Program
					}
					if status.ProgramMode != "" {
						elem["programMode"] = status.ProgramMode
						elem["programFailover"] = status.ProgramFailover
						// The fallback is active when the program is holding the last frame or
						// showing the slate because the source is temporarily offline.
						elem["fallback"] = !status.ProgramSourceActive
					}
					elem["programSwitches"] = status.ProgramSwitches
					if status.ProgramSourceRate != "" {
						elem["programSourceRate"] = status.ProgramSourceRate
					}
					if status.Encoder != "" {
						elem["encoder"] = status.Encoder
					}
					if status.FFmpegArgs != "" {
						elem["ffmpegArgs"] = status.FFmpegArgs
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
					if status.Metrics != nil {
						elem["metrics"] = status.Metrics
					}
					if len(status.Events) > 0 {
						elem["events"] = status.Events
					}
					if len(status.Logs) > 0 {
						elem["logs"] = status.Logs
					}

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
	if v.programWorker != nil {
		v.programWorker.Close()
	}
	return nil
}

func (v *ForwardWorker) Start(ctx context.Context) error {
	wg := &v.wg

	ctx, cancel := context.WithCancel(ctx)
	v.cancel = cancel

	ctx = logger.WithContext(ctx)
	logger.Tf(ctx, "forward start a worker")

	// Start the program worker, which keeps the internal program streams alive.
	if v.programWorker != nil {
		if err := v.programWorker.Start(ctx); err != nil {
			return errors.Wrapf(err, "start program worker")
		}
	}

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
	// The failover mode when the source is offline: empty or "hold" holds the last frame
	// (default), "slate" switches the program to a fallback slate image.
	Failover string `json:"failover"`
	// Whether to transcode (normalize) the output into a regular CFR stream, to mitigate a
	// source with dropped frames, variable frame rate or timestamp jitter. Costs CPU per target.
	Normalize bool `json:"normalize"`
}

func (v *ForwardConfigure) String() string {
	return fmt.Sprintf("platform=%v, stream=%v, server=%v, secret=%v, enabled=%v, customed=%v, label=%v, failover=%v, normalize=%v",
		v.Platform, v.Stream, v.Server, v.Secret, v.Enabled, v.Customed, v.Label, v.Failover, v.Normalize,
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
	v.Failover = u.Failover
	v.Normalize = u.Normalize
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
	// The parsed metrics of the last fanout FFmpeg progress line.
	metrics forwardProgress
	// The first valid progress sample of the current fanout run, to measure media-clock drift.
	metricsFirstSec float64
	metricsFirstAt  time.Time
	// The diagnostics timeline and recent FFmpeg logs (bounded rings).
	logs   []string
	events []ForwardEvent
	// The last fanout FFmpeg command line (secret redacted), for troubleshooting.
	ffmpegArgs string
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

	// The bound source stream and its internal program bus. When bound, the task forwards the
	// internal program stream instead of the source directly, see docs/forward-program-bus.md.
	sourceApp, sourceStream string

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
	Program             string
	ProgramMode         string
	ProgramFailover     string
	ProgramSourceActive bool
	ProgramSourceRate   string
	ProgramSwitches     int
	Encoder             string
	FFmpegArgs          string
	Frame               string
	FrameUpdate         string
	Start               string
	Ready               string
	LastError           string
	LastErrorAt         string
	NextRetryAt         string
	RestartCount        int
	ConsecutiveFailures int
	Metrics             *ForwardMetrics
	Events              []ForwardEvent
	Logs                []string
}

// ForwardEvent is one entry of the diagnostics timeline (task or program).
type ForwardEvent struct {
	At     string `json:"at"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// forwardDiagRingSize bounds the retained diagnostics per forward target.
const forwardDiagRingSize = 50

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

	if v.cancel != nil {
		v.cancel()
	}
	v.State = forwardStateRetrying
	v.NextRetryAt = nil
	v.LastError = ""
	v.LastErrorAt = nil
	v.ConsecutiveFailures = 0
	// A restarted task is a fresh start, reset the accumulated restart counter.
	v.RestartCount = 0

	// Unbind the program, it is re-bound from the reloaded config on the next cycle.
	app, stream := v.sourceApp, v.sourceStream
	v.sourceApp, v.sourceStream = "", ""

	// Reload config from redis.
	if b, err := rdb.HGet(ctx, SRS_FORWARD_CONFIG, v.Platform).Result(); err != nil {
		v.lock.Unlock()
		return errors.Wrapf(err, "hget %v %v", SRS_FORWARD_CONFIG, v.Platform)
	} else if err = json.Unmarshal([]byte(b), v.config); err != nil {
		v.lock.Unlock()
		return errors.Wrapf(err, "unmarshal %v", b)
	}

	v.lock.Unlock()

	// Release the old program outside the lock, to avoid a lock cycle.
	if app != "" && v.forwardWorker != nil && v.forwardWorker.programWorker != nil {
		v.forwardWorker.programWorker.ReleaseProgram(app, stream)
	}

	return nil
}

func (v *ForwardTask) updateFrame(frame string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.frame = strings.TrimSpace(frame)

	now := time.Now()
	v.update = &now

	// Parse the progress line into metrics. Unparsable lines (e.g. error logs) keep the last
	// valid progress, so the metrics never regress to zero on a transient log line.
	if progress, ok := parseFFmpegProgress(v.frame); ok {
		progress.at = now
		if !v.metrics.valid {
			v.metricsFirstSec, v.metricsFirstAt = progress.mediaSec, now
		}
		v.metrics = progress
	}
}

func (v *ForwardTask) queryStatus() ForwardTaskStatus {
	v.lock.Lock()
	defer v.lock.Unlock()

	status := ForwardTaskStatus{
		PID:                 v.PID,
		State:               v.State,
		Input:               v.Input,
		Program:             v.inputStreamURL,
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
	var ps *ProgramStatus
	if v.forwardWorker != nil && v.forwardWorker.programWorker != nil && v.sourceApp != "" {
		if bus := v.forwardWorker.programWorker.GetProgram(v.sourceApp, v.sourceStream); bus != nil {
			p := bus.queryStatus()
			ps = &p
			status.ProgramMode, status.ProgramFailover = ps.Mode, ps.Failover
			status.ProgramSourceActive, status.ProgramSwitches = ps.SourceActive, ps.Switches
			status.ProgramSourceRate = ps.SourceRate
		}
	}
	// Which video encoder this target's pipeline uses: the composite/slate program and the
	// normalize fanout both transcode (hardware NVENC when available, else libx264), while a
	// plain hold + no-normalize pipeline is a zero-transcode copy.
	if v.config != nil && (normalizeForwardFailover(v.config.Failover) == forwardFailoverSlate || v.config.Normalize) {
		if name := forwardVideoEncoderName(); name != "" {
			status.Encoder = name
		}
	} else {
		status.Encoder = "copy"
	}
	status.FFmpegArgs = v.ffmpegArgs
	status.Metrics = v.queryMetrics(ps)
	status.Logs = append([]string(nil), v.logs...)
	status.Events = append([]ForwardEvent(nil), v.events...)
	if ps != nil && len(ps.Events) > 0 {
		status.Events = append(status.Events, ps.Events...)
		sort.Slice(status.Events, func(i, j int) bool { return status.Events[i].At < status.Events[j].At })
	}

	return status
}

// queryMetrics builds the local output-progress metrics. It must be called with v.lock held.
func (v *ForwardTask) queryMetrics(ps *ProgramStatus) *ForwardMetrics {
	if !v.metrics.valid {
		return nil
	}

	m := &ForwardMetrics{
		Frame:           v.metrics.frame,
		OutputFPS:       v.metrics.fps,
		Speed:           v.metrics.speed,
		BitrateKbps:     v.metrics.bitrateKbps,
		SizeBytes:       v.metrics.sizeBytes,
		MediaTime:       v.metrics.mediaTime,
		MediaSecond:     v.metrics.mediaSec,
		NoOutputSeconds: time.Since(v.metrics.at).Seconds(),
	}
	if v.metrics.mediaSec > 0 && v.metrics.frame > 0 {
		m.AvgFPS = float64(v.metrics.frame) / v.metrics.mediaSec
	}
	// The media clock ahead of the wall clock since the first sample of this run. A sustained
	// positive drift indicates timestamp jumps (e.g. after a relay reconnect); ~0 means the
	// output advances at realtime.
	if !v.metricsFirstAt.IsZero() {
		m.MediaClockDrift = (v.metrics.mediaSec - v.metricsFirstSec) - v.metrics.at.Sub(v.metricsFirstAt).Seconds()
	}
	if ps != nil {
		m.RelayReconnects = ps.Reconnects
		m.LastReconnectAt = ps.LastReconnectAt
		m.ReconnectReason = ps.ReconnectReason
	}
	return m
}

// forwardDiagnosticMarkers select the FFmpeg stderr lines worth retaining for diagnostics; the
// banner and routine lines are dropped to keep the API payload small.
var forwardDiagnosticMarkers = []string{
	"Will reconnect", "HTTP error", "Exiting", "Connection refused",
	"Input/output error", "Server returned", "Broken pipe", "No such file",
	"Invalid data", "error", "Error",
}

func isForwardDiagnosticLine(line string) bool {
	for _, m := range forwardDiagnosticMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

func (v *ForwardTask) addLogLocked(line string) {
	line = strings.TrimSpace(line)
	if !isForwardDiagnosticLine(line) {
		return
	}
	if len(line) > 300 {
		line = line[:300]
	}
	v.logs = append(v.logs, fmt.Sprintf("%v %v", time.Now().Format("15:04:05"), line))
	if len(v.logs) > forwardDiagRingSize {
		v.logs = v.logs[len(v.logs)-forwardDiagRingSize:]
	}
}

func (v *ForwardTask) addLog(line string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.addLogLocked(line)
}

func (v *ForwardTask) addEventLocked(name, detail string) {
	v.events = append(v.events, ForwardEvent{At: time.Now().Format(time.RFC3339), Name: name, Detail: detail})
	if len(v.events) > forwardDiagRingSize {
		v.events = v.events[len(v.events)-forwardDiagRingSize:]
	}
}

func (v *ForwardTask) addEvent(name, detail string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	v.addEventLocked(name, detail)
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
	// Reset the output metrics of the previous run.
	v.metrics, v.metricsFirstSec, v.metricsFirstAt = forwardProgress{}, 0, time.Time{}
	v.addEventLocked("start", outputURL)
}

// setFFmpegArgs records the fanout command line for troubleshooting, with any ?secret= value
// masked so credentials are not exposed in the status/API/UI.
func (v *ForwardTask) setFFmpegArgs(args []string) {
	redacted := make([]string, len(args))
	for i, a := range args {
		if j := strings.Index(a, "secret="); j >= 0 {
			if k := strings.IndexAny(a[j:], "& "); k >= 0 {
				a = a[:j] + "secret=***" + a[j+k:]
			} else {
				a = a[:j] + "secret=***"
			}
		}
		redacted[i] = a
	}
	v.lock.Lock()
	defer v.lock.Unlock()
	v.ffmpegArgs = strings.Join(redacted, " ")
}

func (v *ForwardTask) markFailure(err error) time.Duration {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.ConsecutiveFailures++
	v.RestartCount++
	now := time.Now()
	v.LastError = err.Error()
	v.LastErrorAt = &now
	v.addEventLocked("failure", err.Error())
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
	if v.ConsecutiveFailures > 0 {
		v.addEventLocked("recovered", "")
	}
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
		// Never select this task's own output stream, otherwise forwarding would loop.
		ownOutput := v.ownOutputStream()

		var best *SrsStream
		for _, v := range streams {
			var stream SrsStream
			if err := json.Unmarshal([]byte(v), &stream); err != nil {
				return nil, errors.Wrapf(err, "unmarshal %v", v)
			}

			if ownOutput != "" && stream.Stream == ownOutput {
				continue
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
		// Ignore when not enabled, or when the global forward switch is off (a pause that does
		// not change each target's own enabled preference).
		if !v.config.Enabled || !forwardMasterEnabled(ctx) {
			v.releaseProgram()
			v.markHealthy()
			return nil
		}

		// Optional policy: stop the forward when the source has been offline long enough, so a
		// permanently-ended live does not keep the slate (and the forward) alive forever.
		if off, mins := forwardSlateOfflineStop(ctx); off {
			if bus := v.programBus(); bus != nil && bus.sourceInactiveFor() >= time.Duration(mins)*time.Minute {
				logger.Tf(ctx, "forward platform=%v source offline over %vmin, stop", v.Platform, mins)
				v.addEvent("offline-stop", fmt.Sprintf("source offline over %vmin", mins))
				v.releaseProgram()
				v.markWaitingInput()
				return nil
			}
		}

		// Bind the source and its internal program bus on first use. Once bound, we keep
		// forwarding the program even when the source is temporarily offline, because the
		// program bus keeps the program published (the downstream holds the last frame).
		if !v.hasProgram() {
			input, err := selectActiveStream()
			if err != nil {
				return errors.Wrapf(err, "select input")
			}

			if input == nil {
				v.markWaitingInput()
				return nil
			}

			v.bindProgram(ctx, input.App, input.Stream)
		}

		// Start forward task.
		if err := v.doForward(ctx); err != nil {
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

func (v *ForwardTask) hasProgram() bool {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.sourceApp != ""
}

func (v *ForwardTask) programSource() (string, string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.sourceApp, v.sourceStream
}

// forwardEncoderArgs is the video-encoder FFmpeg args, chosen once: NVENC (GPU) when the container
// has a usable GPU, otherwise software libx264. GPU encoding keeps CPU low on the transcode paths
// (normalize, the program slate). NVENC presence is not enough (the GPU may be unusable), so we
// probe it with a tiny encode and fall back on any failure.
var forwardEncoderOnce sync.Once
var forwardEncoderArgs []string

func forwardVideoEncodeArgs() []string {
	forwardEncoderOnce.Do(func() {
		// Use a size above the hardware encoders' minimum: NVENC rejects small frames (e.g. 64x64
		// with "Frame Dimension less than the minimum supported value"), which would make a
		// working GPU look unavailable and silently fall back to libx264.
		probe := exec.CommandContext(context.Background(), "ffmpeg", "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "color=c=black:s=256x256:r=1", "-frames:v", "1",
			"-c:v", "h264_nvenc", "-f", "null", "-")
		if err := probe.Run(); err == nil {
			forwardEncoderArgs = []string{"-c:v", "h264_nvenc", "-preset", "p4", "-tune", "ll", "-profile:v", "high", "-pix_fmt", "yuv420p"}
			logger.Tf(context.Background(), "forward: using NVENC (GPU) H.264 encoder")
		} else {
			forwardEncoderArgs = []string{"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "high", "-pix_fmt", "yuv420p"}
			logger.Tf(context.Background(), "forward: NVENC unavailable, using libx264")
		}
	})
	return forwardEncoderArgs
}

// forwardVideoEncoderName reports the resolved video encoder for status, e.g. "h264_nvenc"
// (hardware) or "libx264" (software).
func forwardVideoEncoderName() string {
	args := forwardVideoEncodeArgs()
	if len(args) >= 2 && args[0] == "-c:v" {
		return args[1]
	}
	return ""
}

// forwardKeyframeIntervalSeconds is the target GOP duration; platforms generally require a
// keyframe interval of at most 2 seconds.
const forwardKeyframeIntervalSeconds = 2

// forwardGOPFrames computes the GOP in frames from the source's average frame rate, so the
// keyframe interval stays the required duration. A hardcoded -g 50 is 2s at 25fps but only 0.83s
// at 60fps, which adds I-frames and causes bitrate spikes.
func forwardGOPFrames(rate string, seconds int) int {
	fps := 25.0
	if num, den, ok := parseFPS(rate); ok && den > 0 && num > 0 {
		if f := float64(num) / float64(den); f >= 1 && f <= 120 {
			fps = f
		}
	}
	if seconds <= 0 || seconds > 10 {
		seconds = forwardKeyframeIntervalSeconds
	}
	gop := int(fps*float64(seconds) + 0.5)
	if gop < 1 {
		gop = 1
	}
	if gop > 1000 {
		gop = 1000
	}
	return gop
}

// forwardFanoutArgs builds the FFmpeg arguments for the fanout. When normalize is on, the
// program is transcoded into a regular CFR stream (software H.264 + AAC) so a source with
// dropped frames, variable frame rate or timestamp jitter is regularized; otherwise it is a
// zero-transcode `-c copy`. The input reads the program via HTTP-FLV with reconnect.
func forwardFanoutArgs(normalize bool, gop int, programInputURL, outputURL string) []string {
	args := []string{
		"-reconnect", "1", "-reconnect_at_eof", "1", "-reconnect_streamed", "1",
		"-reconnect_on_http_error", forwardReconnectHTTPError,
		"-reconnect_delay_max", fmt.Sprintf("%v", forwardReconnectDelayMax),
	}

	if normalize {
		args = append(args,
			"-fflags", "+genpts",
			"-i", programInputURL,
			"-fps_mode", "cfr",
		)
		args = append(args, forwardVideoEncodeArgs()...)
		if gop <= 0 {
			gop = 50
		}
		args = append(args,
			"-g", fmt.Sprintf("%d", gop), "-keyint_min", fmt.Sprintf("%d", gop), "-sc_threshold", "0",
			"-c:a", "aac", "-b:a", "128k", "-ar", "44100",
		)
	} else {
		args = append(args, "-i", programInputURL, "-c", "copy")
	}

	// If RTMP use flv, if SRT use mpegts, otherwise do not set.
	if strings.HasPrefix(outputURL, "rtmp://") || strings.HasPrefix(outputURL, "rtmps://") {
		args = append(args, "-f", "flv")
	} else if strings.HasPrefix(outputURL, "srt://") {
		args = append(args, "-pes_payload_size", "0", "-f", "mpegts")
	}
	args = append(args, outputURL)

	return args
}

// programBus returns the program bus of the bound source, or nil.
func (v *ForwardTask) programBus() *ProgramBus {
	v.lock.Lock()
	defer v.lock.Unlock()
	if v.forwardWorker == nil || v.forwardWorker.programWorker == nil || v.sourceApp == "" {
		return nil
	}
	return v.forwardWorker.programWorker.GetProgram(v.sourceApp, v.sourceStream)
}

// waitStreamReady polls the HTTP-FLV URL until it returns 200, or the timeout is reached.
func waitStreamReady(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		err := func() error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return errors.Errorf("status %v", resp.StatusCode)
			}
			return nil
		}()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return errors.Wrapf(ctx.Err(), "wait %v", url)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// ownOutputStream returns the local stream name this task publishes to, used to avoid
// selecting our own output stream as the source (which would loop). It only makes sense for
// a local target, so it returns empty for remote platforms.
func (v *ForwardTask) ownOutputStream() string {
	v.lock.Lock()
	defer v.lock.Unlock()

	if v.config == nil || !strings.Contains(v.config.Server, "localhost") {
		return ""
	}

	secret := v.config.Secret
	if i := strings.Index(secret, "?"); i >= 0 {
		secret = secret[:i]
	}
	full := fmt.Sprintf("%v/%v", strings.TrimSuffix(v.config.Server, "/"), strings.TrimPrefix(secret, "/"))
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
}

// bindProgram binds the source stream and starts (or joins) its internal program bus, so the
// task can forward a stable program stream instead of the volatile source.
func (v *ForwardTask) bindProgram(ctx context.Context, app, stream string) {
	v.lock.Lock()
	v.sourceApp, v.sourceStream = app, stream
	v.lock.Unlock()

	failover := ""
	if v.config != nil {
		failover = v.config.Failover
	}
	if v.forwardWorker != nil && v.forwardWorker.programWorker != nil {
		v.forwardWorker.programWorker.EnsureProgram(app, stream, failover)
	}

	logger.Tf(ctx, "forward bind program, platform=%v, source=%v/%v, program=%v, failover=%v",
		v.Platform, app, stream, forwardProgramStreamName(app, stream), failover)
}

// releaseProgram drops the program bus reference, called when the task stops forwarding.
func (v *ForwardTask) releaseProgram() {
	v.lock.Lock()
	app, stream := v.sourceApp, v.sourceStream
	v.sourceApp, v.sourceStream = "", ""
	v.lock.Unlock()

	if app != "" && v.forwardWorker != nil && v.forwardWorker.programWorker != nil {
		v.forwardWorker.programWorker.ReleaseProgram(app, stream)
	}
}

func (v *ForwardTask) doForward(ctx context.Context) error {
	// Create context for current task.
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	v.cancel = cancel

	// Stop the forward when the source has been offline long enough, if configured. The run loop
	// only re-checks between FFmpeg runs, so a running fanout must be cancelled explicitly.
	if off, mins := forwardSlateOfflineStop(ctx); off {
		if bus := v.programBus(); bus != nil {
			go func() {
				ticker := time.NewTicker(10 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if bus.sourceInactiveFor() >= time.Duration(mins)*time.Minute {
							logger.Tf(ctx, "forward platform=%v source offline over %vmin, cancel", v.Platform, mins)
							cancel()
							return
						}
					}
				}
			}()
		}
	}

	// The program bus keeps the internal program stream alive, so we always read from the
	// program instead of the source directly, see docs/forward-program-bus.md.
	sourceApp, sourceStream := v.programSource()
	host := "localhost"
	programStream := forwardProgramStreamName(sourceApp, sourceStream)
	programURL := fmt.Sprintf("rtmp://%v/%v/%v", host, sourceApp, programStream)
	sourceURL := fmt.Sprintf("%v/%v", sourceApp, sourceStream)
	// Read the program as HTTP-FLV with reconnect (HTTP is the only protocol FFmpeg can
	// auto-reconnect), so a program publisher handoff (e.g. the fallback slate) or a relay
	// restart does not kill the fanout and drop the downstream: the fanout keeps its output
	// connection and simply holds the last frame until the input comes back.
	programInputURL := fmt.Sprintf("http://localhost:%v/%v/%v.flv", forwardProgramHTTPPort, sourceApp, programStream)

	// Build output URL.
	outputServer := strings.ReplaceAll(v.config.Server, "localhost", host)
	if !strings.HasSuffix(outputServer, "/") && !strings.HasPrefix(v.config.Secret, "/") && v.config.Secret != "" {
		outputServer += "/"
	}
	outputURL := fmt.Sprintf("%v%v", outputServer, v.config.Secret)
	// A local target on this SRS still needs publish authentication, but its secret must stay a
	// clean stream name: if ?secret= were part of the secret field, SRS would parse the stream
	// wrongly (e.g. the stream becomes the secret value). Append the local publish secret here.
	if strings.Contains(v.config.Server, "localhost") && !strings.Contains(outputURL, "?secret=") {
		if pub := forwardPublishSecret(ctx); pub != "" {
			outputURL += "?secret=" + pub
		}
	}

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

	// Create a heartbeat to poll and manage the status of FFmpeg process. The fanout reads the
	// program, which holds the last frame during a source outage, so use a longer no-update
	// timeout to avoid killing the fanout and dropping the downstream.
	heartbeat := NewFFmpegHeartbeat(cancel, func(h *FFmpegHeartbeat) {
		h.MaxNoUpdateTime = forwardFanoutNoUpdateTime
	})
	v.starttime, v.firstReadyTime = &heartbeat.starttime, nil
	defer func() {
		v.starttime = nil
	}()

	// Wait for the program to be published before reading it. The fanout reads HTTP-FLV, whose
	// initial connection fails hard (FFmpeg does not auto-retry a failed initial open) when the
	// program is not published yet (the platform proxy returns 502), which would cause a
	// spurious restart.
	if err := waitStreamReady(ctx, programInputURL, 10*time.Second); err != nil {
		return errors.Wrapf(err, "wait program %v ready", programInputURL)
	}

	// Build the FFmpeg arguments. Optionally normalize (transcode) the output into a regular
	// CFR stream, to mitigate a source with dropped frames, VFR or timestamp jitter.
	// Keep the keyframe interval at 2s of the output's frame rate instead of a fixed 50 frames.
	gop := 50
	if bus := v.programBus(); bus != nil {
		_, _, rate := bus.sourceSize()
		gop = forwardGOPFrames(rate, forwardSlateConfigLoad(ctx).KeyframeSeconds)
	}
	args := forwardFanoutArgs(v.config != nil && v.config.Normalize, gop, programInputURL, outputURL)
	v.setFFmpegArgs(args)
	// Create the command object.
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.Wrapf(err, "pipe process")
	}

	if err := cmd.Start(); err != nil {
		return errors.Wrapf(err, "execute ffmpeg %v", strings.Join(args, " "))
	}

	v.markRunning(int32(cmd.Process.Pid), sourceURL, programURL, outputURL)
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
	logger.Tf(ctx, "forward start, platform=%v, stream=%v, program=%v, pid=%v",
		v.Platform, sourceURL, programURL, v.PID)

	if err := v.saveTask(ctx); err != nil {
		return errors.Wrapf(err, "save task %v", v.String())
	}

	// Note that we no longer watch the source stream here. The program bus keeps the program
	// published across source outages (the downstream holds the last frame), so the FFmpeg
	// output persists until it fails or is cancelled, keeping the downstream session alive.

	// Pull the latest log frame.
	heartbeat.Polling(ctx, stderr)
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.firstReadyCtx.Done():
			v.firstReadyTime = &heartbeat.firstReadyTime
			v.addEvent("ready", "")
		}

		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-heartbeat.FrameLogs:
				v.updateFrame(frame)
			case line := <-heartbeat.ExtraLogs:
				v.addLog(line)
			}
		}
	}()

	// Experiment C: SRS keeps consumers across a publisher change (SrsLiveSource::on_unpublish
	// does not touch consumers), so a fanout should ride through the brief program handoff
	// instead of being restarted. Restarting the fanout here was the main cause of the multi
	// second downstream gap. Set forwardFanoutRestartOnSwitch to true to restore the old behavior.
	if forwardFanoutRestartOnSwitch {
		if bus := v.programBus(); bus != nil {
			startSwitches := bus.queryStatus().Switches
			go func() {
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if bus.queryStatus().Switches != startSwitches {
							logger.Tf(ctx, "forward: program publisher changed, restart fanout, platform=%v", v.Platform)
							cancel()
							return
						}
					}
				}
			}()
		}
	}

	// Process terminated, or user cancel the process.
	select {
	case <-parentCtx.Done():
	case <-ctx.Done():
	case <-heartbeat.PollingCtx.Done():
	}
	logger.Tf(ctx, "Forward: Cycle stopping, platform=%v, stream=%v, pid=%v",
		v.Platform, sourceURL, v.PID)

	err = cmd.Wait()
	logger.Tf(ctx, "forward done, platform=%v, stream=%v, pid=%v, err=%v",
		v.Platform, sourceURL, v.PID, err,
	)

	return err
}
