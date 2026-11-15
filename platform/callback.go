// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	// From ossrs.
	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"

	// Use v8 because we use Go 1.16+, while v9 requires Go 1.18+
	"github.com/go-redis/redis/v8"
)

var callbackWorker *CallbackWorker

type CallbackWorker struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// The ephemeral callback config.
	ephemeralConfig CallbackConfig
	// Whether update the config immediately.
	updateConfig chan bool

	lock sync.Mutex
}

func NewCallbackWorker() *CallbackWorker {
	return &CallbackWorker{
		updateConfig: make(chan bool, 1),
	}
}

func (v *CallbackWorker) Handle(ctx context.Context, handler *http.ServeMux) error {
	ep := "/terraform/v1/mgmt/hooks/query"
	logger.Tf(ctx, "Handle %v", ep)

	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var config CallbackConfig
			if err := config.Load(ctx); err != nil {
				return errors.Wrapf(err, "load")
			}

			req, err := rdb.HGet(ctx, SRS_HOOKS, "req").Result()
			if err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hget %v req", SRS_HOOKS)
			}

			res, err := rdb.HGet(ctx, SRS_HOOKS, "res").Result()
			if err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hget %v res", SRS_HOOKS)
			}

			type HooksQueryResult struct {
				Request  string `json:"req"`
				Response string `json:"res"`
				*CallbackConfig
			}
			ohttp.WriteData(ctx, w, r, &HooksQueryResult{
				Request:        req,
				Response:       res,
				CallbackConfig: &config,
			})
			logger.Tf(ctx, "hooks apply ok, %v", config.String())
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	ep = "/terraform/v1/mgmt/hooks/apply"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var config CallbackConfig
			if err := ParseBody(ctx, r.Body, &struct {
				*CallbackConfig
			}{
				CallbackConfig: &config,
			}); err != nil {
				return errors.Wrapf(err, "parse body")
			}

			if err := rdb.HSet(ctx, SRS_HOOKS, "target", config.Target).Err(); err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hset %v target %v", SRS_HOOKS, config.Target)
			}
			if err := rdb.HSet(ctx, SRS_HOOKS, "opaque", config.Opaque).Err(); err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hset %v opaque %v", SRS_HOOKS, config.Opaque)
			}
			if err := rdb.HSet(ctx, SRS_HOOKS, "all", fmt.Sprintf("%v", config.All)).Err(); err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hset %v all %v", SRS_HOOKS, config.All)
			}

			// Use the request host as the default host.
			if config.Host == "" {
				config.Host = fmt.Sprintf("http://%v", r.Host)
				if r.TLS != nil {
					config.Host = fmt.Sprintf("https://%v", r.Host)
				}
			}
			if err := rdb.HSet(ctx, SRS_HOOKS, "host", config.Host).Err(); err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hset %v host %v", SRS_HOOKS, config.Host)
			}

			// Notify the callback worker to update the config.
			select {
			case v.updateConfig <- true:
			case <-ctx.Done():
			default:
			}

			ohttp.WriteData(ctx, w, r, nil)
			logger.Tf(ctx, "hooks apply ok, %v", config.String())
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	ep = "/terraform/v1/mgmt/hooks/example"
	logger.Tf(ctx, "Handle %v", ep)
	handler.HandleFunc(ep, func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			q := r.URL.Query()

			var fail bool
			if value := q.Get("fail"); value == "true" || value == "1" {
				fail = true
			}

			var action, opaque string
			if err := ParseBody(ctx, r.Body, &struct {
				Action *string `json:"action"`
				Opaque *string `json:"opaque"`
			}{
				Action: &action,
				Opaque: &opaque,
			}); err != nil {
				return errors.Wrapf(err, "parse body")
			}

			if fail {
				return errors.Errorf("fail as required, action=%v, opaque=%v", action, opaque)
			}

			ohttp.WriteData(ctx, w, r, nil)
			logger.Tf(ctx, "hooks example ok, action=%v, opaque=%v", action, opaque)
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})

	ep = "/terraform/v1/mgmt/callback/query"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var config CallbackConfig
			if err := config.Load(ctx); err != nil {
				return errors.Wrapf(err, "load")
			}

			stats := map[string]string{}
			if res, err := rdb.HGetAll(ctx, SRS_CALLBACK_STATS).Result(); err == nil {
				stats = res
			}

			var events []callbackRecord
			if res, err := rdb.LRange(ctx, SRS_CALLBACK_EVENTS, 0, 19).Result(); err == nil {
				for _, s := range res {
					var rec callbackRecord
					if json.Unmarshal([]byte(s), &rec) == nil {
						events = append(events, rec)
					}
				}
			}

			type CallbackQueryResult struct {
				Enabled bool              `json:"enabled"`
				Target  string            `json:"target"`
				All     bool              `json:"all"`
				Stats   map[string]string `json:"stats"`
				Events  []callbackRecord  `json:"events"`
			}
			ohttp.WriteData(ctx, w, r, &CallbackQueryResult{
				Enabled: config.All && config.Target != "",
				Target:  maskCallbackTarget(config.Target),
				All:     config.All,
				Stats:   stats,
				Events:  events,
			})
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	// Sends one synthetic event through the real path, so "is the target reachable and does it
	// accept our payload" is answerable without publishing a stream.
	ep = "/terraform/v1/mgmt/callback/test"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var config CallbackConfig
			if err := config.Load(ctx); err != nil {
				return errors.Wrapf(err, "load")
			}
			if config.Target == "" {
				return errors.Errorf("callback target is empty, apply a target first")
			}

			rec := sendCallbackTest(ctx, config)

			// The test is a probe, so its failure must be reported as data, not as an API error:
			// the caller needs the status and the reason to decide what to fix.
			ohttp.WriteData(ctx, w, r, rec)
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))

	return nil
}

// sendCallbackTest posts a synthetic callback event and returns the recorded outcome.
func sendCallbackTest(ctx context.Context, config CallbackConfig) callbackRecord {
	payload, _ := json.Marshal(&struct {
		RequestID string `json:"request_id"`
		Action    string `json:"action"`
		Opaque    string `json:"opaque"`
	}{
		RequestID: uuid.NewString(),
		Action:    "test",
		Opaque:    config.Opaque,
	})

	rec := callbackRecord{Action: "test", Target: maskCallbackTarget(config.Target), Time: time.Now().UnixMilli()}
	started := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Target, bytes.NewReader(payload))
	if err != nil {
		rec.Result, rec.Error = "send-error", err.Error()
		rec.LatencyMS = time.Since(started).Milliseconds()
		recordCallback(ctx, rec)
		return rec
	}
	req.Header.Set("Content-Type", "application/json")

	var res *http.Response
	if strings.HasPrefix(config.Target, "https://") {
		client := &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: envTLSSkipVerify()},
			},
		}
		res, err = client.Do(req)
	} else {
		res, err = (&http.Client{Timeout: 10 * time.Second}).Do(req)
	}
	rec.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		rec.Result, rec.Error = "send-error", err.Error()
		recordCallback(ctx, rec)
		return rec
	}
	defer res.Body.Close()
	rec.Status = res.StatusCode

	body, _ := ioutil.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		rec.Result = "http-error"
		rec.Error = fmt.Sprintf("response status %v: %v", res.StatusCode, strings.TrimSpace(string(body)))
		recordCallback(ctx, rec)
		return rec
	}

	// Same acceptance rule as the real callback: a JSON body must carry code 0, otherwise the
	// partner received the event but rejected it.
	var code int
	if err := json.Unmarshal(body, &struct {
		Code *int `json:"code"`
	}{Code: &code}); err == nil && code != 0 {
		rec.Result = "code-error"
		rec.Error = fmt.Sprintf("response code %v: %v", code, strings.TrimSpace(string(body)))
		recordCallback(ctx, rec)
		return rec
	}

	rec.Result = "ok"
	recordCallback(ctx, rec)
	return rec
}

func (v *CallbackWorker) Close() error {
	if v.cancel != nil {
		v.cancel()
	}
	v.wg.Wait()
	return nil
}

func (v *CallbackWorker) Start(ctx context.Context) error {
	wg := &v.wg

	ctx, cancel := context.WithCancel(ctx)
	v.cancel = cancel

	ctx = logger.WithContext(ctx)
	logger.Tf(ctx, "callback start a worker")

	wg.Add(1)
	go func() {
		defer wg.Done()

		for ctx.Err() == nil {
			var config CallbackConfig
			if err := config.Load(ctx); err != nil {
				logger.Wf(ctx, "load config %v err %+v", config, err)

				select {
				case <-ctx.Done():
				case <-time.After(10 * time.Second):
				}
				continue
			}

			func() {
				v.lock.Lock()
				defer v.lock.Unlock()
				v.ephemeralConfig = config
			}()

			select {
			case <-ctx.Done():
			case <-time.After(time.Second * 3):
			case <-v.updateConfig:
			}
		}
	}()

	return nil
}

// Callback delivery is fire-and-forget today: failures only reach the container log, so a
// partner integration that silently rejects every event is invisible from the console. These
// records keep the last N attempts (and the aggregate counters) so the 平台回呼 card can answer
// event -> cause -> effect without reading docker logs.
const (
	SRS_CALLBACK_EVENTS = "SRS_CALLBACK_EVENTS"
	SRS_CALLBACK_STATS  = "SRS_CALLBACK_STATS"
	callbackEventLimit  = 50
)

// callbackRecord is one outbound callback attempt.
type callbackRecord struct {
	Time      int64  `json:"time"`
	Action    string `json:"action"`
	Stream    string `json:"stream,omitempty"`
	Target    string `json:"target"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latencyMS"`
	// Result is a stable code the UI translates: ok, http-error, code-error, send-error.
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// maskCallbackTarget keeps the scheme and host (which is what you need to verify routing) and
// hides the path and query, which is where callback tokens usually live.
func maskCallbackTarget(target string) string {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return "***"
	}
	return fmt.Sprintf("%v://%v/***", u.Scheme, u.Host)
}

func recordCallback(ctx context.Context, rec callbackRecord) {
	if rdb == nil {
		return
	}
	if rec.Time == 0 {
		rec.Time = time.Now().UnixMilli()
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	if err := rdb.LPush(ctx, SRS_CALLBACK_EVENTS, string(b)).Err(); err != nil && err != redis.Nil {
		logger.Wf(ctx, "callback record push err %v", err)
		return
	}
	rdb.LTrim(ctx, SRS_CALLBACK_EVENTS, 0, callbackEventLimit-1)

	rdb.HIncrBy(ctx, SRS_CALLBACK_STATS, "total", 1)
	if rec.Result != "ok" {
		rdb.HIncrBy(ctx, SRS_CALLBACK_STATS, "failed", 1)
	}
	rdb.HSet(ctx, SRS_CALLBACK_STATS, "lastAt", rec.Time)
	rdb.HSet(ctx, SRS_CALLBACK_STATS, "lastResult", rec.Result)
	rdb.HSet(ctx, SRS_CALLBACK_STATS, "lastStatus", rec.Status)
	rdb.HSet(ctx, SRS_CALLBACK_STATS, "lastLatencyMS", rec.LatencyMS)
	if rec.Error != "" {
		rdb.HSet(ctx, SRS_CALLBACK_STATS, "lastError", rec.Error)
	}
}

func (v *CallbackWorker) OnStreamMessage(ctx context.Context, action SrsAction, streamObj *SrsStream) error {
	if action != SrsActionOnPublish && action != SrsActionOnUnpublish {
		return nil
	}

	var config CallbackConfig
	func() {
		v.lock.Lock()
		defer v.lock.Unlock()
		config = v.ephemeralConfig
	}()

	if !config.All || config.Target == "" {
		return nil
	}

	req := &struct {
		RequestID string `json:"request_id"`
		// The callback parameters.
		Action string `json:"action"`
		Opaque string `json:"opaque"`
		Vhost  string `json:"vhost,omitempty"`
		App    string `json:"app,omitempty"`
		Stream string `json:"stream,omitempty"`
		Param  string `json:"param,omitempty"`
	}{
		RequestID: uuid.NewString(),
		// The callback parameters.
		Action: string(action),
		Opaque: config.Opaque,
		Vhost:  streamObj.Vhost,
		App:    streamObj.App,
		Stream: streamObj.Stream,
	}
	if action == SrsActionOnPublish {
		req.Param = streamObj.Param
	}

	pfn4 := func(b, b2 []byte, code int) error {
		if code != 0 {
			return errors.Errorf("response code %v", code)
		}

		logger.Tf(ctx, "callback ok, post %v with %s, response %v", config.String(), string(b), string(b2))
		return nil
	}

	pfn3 := func(b, b2 []byte) error {
		if code, err := strconv.Atoi(string(b2)); err == nil {
			return pfn4(b, b2, int(code))
		}

		var code int
		if err := json.Unmarshal(b2, &struct {
			Code *int `json:"code"`
		}{
			Code: &code,
		}); err != nil {
			return errors.Wrapf(err, "unmarshal response")
		}
		return pfn4(b, b2, code)
	}

	pfn2 := func(b []byte) error {
		started := time.Now()
		rec := callbackRecord{
			Action: string(action),
			Stream: fmt.Sprintf("%v/%v", streamObj.App, streamObj.Stream),
			Target: maskCallbackTarget(config.Target),
		}
		finish := func(result string, status int, cause error) error {
			rec.Result, rec.Status = result, status
			rec.LatencyMS = time.Since(started).Milliseconds()
			if cause != nil {
				rec.Error = cause.Error()
			}
			recordCallback(ctx, rec)
			return cause
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Target, bytes.NewReader(b))
		if err != nil {
			return finish("send-error", 0, errors.Wrapf(err, "new request"))
		}

		req.Header.Set("Content-Type", "application/json")

		var res *http.Response
		if strings.HasPrefix(config.Target, "https://") {
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: envTLSSkipVerify(),
					},
				},
			}
			res, err = client.Do(req)
		} else {
			res, err = http.DefaultClient.Do(req)
		}
		if err != nil {
			return finish("send-error", 0, errors.Wrapf(err, "http post"))
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return finish("http-error", res.StatusCode, errors.Errorf("response status %v", res.StatusCode))
		}

		b2, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return errors.Wrapf(err, "read body")
		}

		if err := rdb.HSet(ctx, SRS_HOOKS, "res", string(b2)).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hset %v res %v", SRS_HOOKS, string(b2))
		}

		if err := pfn3(b, b2); err != nil {
			return finish("code-error", res.StatusCode, errors.Wrapf(err, "res body %v", string(b2)))
		}

		return finish("ok", res.StatusCode, nil)
	}

	pfn := func() error {
		b, err := json.Marshal(req)
		if err != nil {
			return errors.Wrapf(err, "marshal req")
		}

		if err := rdb.HSet(ctx, SRS_HOOKS, "req", string(b)).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hset %v req %v", SRS_HOOKS, string(b))
		}

		if err := pfn2(b); err != nil {
			return errors.Wrapf(err, "post with %s", string(b))
		}

		return nil
	}

	if err := pfn(); err != nil {
		return errors.Wrapf(err, "callback with conf %v, req %v", config.String(), req)
	}
	return nil
}

func (v *CallbackWorker) OnRecordMessage(ctx context.Context, action SrsAction, taskUUID string, message *SrsOnHlsMessage, artifact *M3u8VoDArtifact) error {
	if action != SrsActionOnRecordBegin && action != SrsActionOnRecordEnd {
		return nil
	}
	if action == SrsActionOnRecordEnd && artifact == nil {
		return fmt.Errorf("artifact should not be nil")
	}

	var config CallbackConfig
	func() {
		v.lock.Lock()
		defer v.lock.Unlock()
		config = v.ephemeralConfig
	}()

	if !config.All || config.Target == "" {
		return nil
	}

	req := &struct {
		RequestID string `json:"request_id"`
		// The callback parameters.
		Action       string `json:"action"`
		Opaque       string `json:"opaque"`
		Vhost        string `json:"vhost,omitempty"`
		App          string `json:"app,omitempty"`
		Stream       string `json:"stream,omitempty"`
		UUID         string `json:"uuid,omitempty"`
		ArtifactCode *int   `json:"artifact_code,omitempty"`
		ArtifactPath string `json:"artifact_path,omitempty"`
		ArtifactURL  string `json:"artifact_url,omitempty"`
	}{
		RequestID: uuid.NewString(),
		// The callback parameters.
		Action: string(action),
		Opaque: config.Opaque,
		UUID:   taskUUID,
		Vhost:  message.Vhost,
		App:    message.App,
		Stream: message.Stream,
	}

	if action == SrsActionOnRecordEnd {
		code := 0
		if artifact.Processing {
			code = int(SrsStackErrorCallbackRecord)
		}
		req.ArtifactCode = &code
		req.ArtifactPath = fmt.Sprintf("%v/record/%v/index.mp4", serverDataDirectory, artifact.UUID)
		req.ArtifactURL = fmt.Sprintf("%v/terraform/v1/hooks/record/hls/%v/index.mp4", config.Host, artifact.UUID)
	}

	pfn4 := func(b, b2 []byte, code int) error {
		if code != 0 {
			return errors.Errorf("response code %v", code)
		}

		logger.Tf(ctx, "callback ok, post %v with %s, response %v", config.String(), string(b), string(b2))
		return nil
	}

	pfn3 := func(b, b2 []byte) error {
		if code, err := strconv.Atoi(string(b2)); err == nil {
			return pfn4(b, b2, int(code))
		}

		var code int
		if err := json.Unmarshal(b2, &struct {
			Code *int `json:"code"`
		}{
			Code: &code,
		}); err != nil {
			return errors.Wrapf(err, "unmarshal response")
		}
		return pfn4(b, b2, code)
	}

	pfn2 := func(b []byte) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Target, bytes.NewReader(b))
		if err != nil {
			return errors.Wrapf(err, "new request")
		}

		req.Header.Set("Content-Type", "application/json")

		var res *http.Response
		if strings.HasPrefix(config.Target, "https://") {
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: envTLSSkipVerify(),
					},
				},
			}
			res, err = client.Do(req)
		} else {
			res, err = http.DefaultClient.Do(req)
		}
		if err != nil {
			return errors.Wrapf(err, "http post")
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return errors.Errorf("response status %v", res.StatusCode)
		}

		b2, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return errors.Wrapf(err, "read body")
		}

		if err := rdb.HSet(ctx, SRS_HOOKS, "res", string(b2)).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hset %v res %v", SRS_HOOKS, string(b2))
		}

		if err := pfn3(b, b2); err != nil {
			return errors.Wrapf(err, "res body %v", string(b2))
		}

		return nil
	}

	pfn := func() error {
		b, err := json.Marshal(req)
		if err != nil {
			return errors.Wrapf(err, "marshal req")
		}

		if err := rdb.HSet(ctx, SRS_HOOKS, "req", string(b)).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hset %v req %v", SRS_HOOKS, string(b))
		}

		if err := pfn2(b); err != nil {
			return errors.Wrapf(err, "post with %s", string(b))
		}

		return nil
	}

	if err := pfn(); err != nil {
		return errors.Wrapf(err, "callback with conf %v, req %v", config.String(), req)
	}
	return nil
}

func (v *CallbackWorker) OnOCR(ctx context.Context, action SrsAction, taskUUID string, message *SrsOnHlsMessage, prompt, result string) error {
	if action != SrsActionOnOcr {
		return nil
	}

	var config CallbackConfig
	func() {
		v.lock.Lock()
		defer v.lock.Unlock()
		config = v.ephemeralConfig
	}()

	if !config.All || config.Target == "" {
		return nil
	}

	req := &struct {
		RequestID string `json:"request_id"`
		// The callback parameters.
		Action string `json:"action"`
		Opaque string `json:"opaque"`
		Vhost  string `json:"vhost,omitempty"`
		App    string `json:"app,omitempty"`
		Stream string `json:"stream,omitempty"`
		// The OCR task UUID.
		UUID string `json:"uuid,omitempty"`
		// The OCR prompt.
		Prompt string `json:"prompt,omitempty"`
		// The OCR result.
		Result string `json:"result,omitempty"`
	}{
		RequestID: uuid.NewString(),
		// The callback parameters.
		Action: string(action),
		Opaque: config.Opaque,
		Vhost:  message.Vhost,
		App:    message.App,
		Stream: message.Stream,
		// The OCR task UUID.
		UUID: taskUUID,
		// The OCR prompt.
		Prompt: prompt,
		// The OCR result.
		Result: result,
	}

	pfn4 := func(b, b2 []byte, code int) error {
		if code != 0 {
			return errors.Errorf("response code %v", code)
		}

		logger.Tf(ctx, "callback ok, post %v with %s, response %v", config.String(), string(b), string(b2))
		return nil
	}

	pfn3 := func(b, b2 []byte) error {
		if code, err := strconv.Atoi(string(b2)); err == nil {
			return pfn4(b, b2, int(code))
		}

		var code int

		if err := json.Unmarshal(b2, &struct {
			Code *int `json:"code"`
		}{
			Code: &code,
		}); err != nil {
			return errors.Wrapf(err, "unmarshal response")
		}
		return pfn4(b, b2, code)
	}

	pfn2 := func(b []byte) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Target, bytes.NewReader(b))
		if err != nil {
			return errors.Wrapf(err, "new request")
		}

		req.Header.Set("Content-Type", "application/json")

		var res *http.Response
		if strings.HasPrefix(config.Target, "https://") {
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: envTLSSkipVerify(),
					},
				},
			}
			res, err = client.Do(req)
		} else {
			res, err = http.DefaultClient.Do(req)
		}
		if err != nil {
			return errors.Wrapf(err, "http post")
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return errors.Errorf("response status %v", res.StatusCode)
		}

		b2, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return errors.Wrapf(err, "read body")
		}

		if err := rdb.HSet(ctx, SRS_HOOKS, "res", string(b2)).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hset %v res %v", SRS_HOOKS, string(b2))
		}

		if err := pfn3(b, b2); err != nil {
			return errors.Wrapf(err, "res body %v", string(b2))
		}

		return nil
	}

	pfn := func() error {
		b, err := json.Marshal(req)
		if err != nil {
			return errors.Wrapf(err, "marshal req")
		}

		if err := rdb.HSet(ctx, SRS_HOOKS, "req", string(b)).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hset %v req %v", SRS_HOOKS, string(b))
		}

		if err := pfn2(b); err != nil {
			return errors.Wrapf(err, "post with %s", string(b))
		}

		return nil
	}

	if err := pfn(); err != nil {
		return errors.Wrapf(err, "callback with conf %v, req %v", config.String(), req)
	}
	return nil
}

type CallbackConfig struct {
	// The callback target.
	Target string `json:"target"`
	// The opaque string, for example, the token.
	Opaque string `json:"opaque"`
	// Whether to callback all streams.
	All bool `json:"all"`
	// The full host to generate the full URl for callback.
	Host string `json:"host"`
}

func (v CallbackConfig) String() string {
	return fmt.Sprintf("target=%v, opaque=%v, all=%v, host=%v",
		v.Target, v.Opaque, v.All, v.Host)
}

func (v *CallbackConfig) Load(ctx context.Context) (err error) {
	if v.Target, err = rdb.HGet(ctx, SRS_HOOKS, "target").Result(); err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hget %v target", SRS_HOOKS)
	}

	if v.Opaque, err = rdb.HGet(ctx, SRS_HOOKS, "opaque").Result(); err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hget %v opaque", SRS_HOOKS)
	}

	if all, err := rdb.HGet(ctx, SRS_HOOKS, "all").Result(); err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hget %v all", SRS_HOOKS)
	} else if all == "true" {
		v.All = true
	}

	if v.Host, err = rdb.HGet(ctx, SRS_HOOKS, "host").Result(); err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hget %v host", SRS_HOOKS)
	}

	return nil
}
