package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"
)

func TestScenario_WithStream_PublishVliveStreamUrl(t *testing.T) {
	tc(t, "虛擬直播：以 URL 為來源開播的全流程")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	defer cancel()
	select {
	case <-ctx.Done():
		return
	case <-ffmpeg.ReadyCtx().Done():
	}

	// Use the publish stream url as the vlive input.
	res := struct {
		Name   string `json:"name"`
		Target string `json:"target"`
		UUID   string `json:"uuid"`
		Size   int64  `json:"size"`
		Type   string `json:"type"`
	}{}
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/stream-url", &struct {
		StreamURL string `json:"url"`
	}{
		StreamURL: streamURL,
	}, &res); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive streamUrl failed")
		return
	}

	// Use the publish stream url as the vlive source.
	res.Size = 0
	res.Type = "stream"
	codec := struct {
		UUID  string `json:"uuid"`
		Audio struct {
			CodecName  string `json:"codec_name"`
			Channels   int    `json:"channels"`
			SampleRate string `json:"sample_rate"`
		} `json:"audio"`
		Video struct {
			CodecName string `json:"codec_name"`
			Profile   string `json:"profile"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"video"`
	}{}
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/source", &struct {
		Platform string        `json:"platform"`
		Files    []interface{} `json:"files"`
	}{
		Platform: "bilibili",
		Files:    []interface{}{res},
	}, &struct {
		Files []interface{} `json:"files"`
	}{
		Files: []interface{}{&codec},
	}); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive source failed")
		return
	}

	if err := func() error {
		if codec.UUID != res.UUID {
			return errors.Errorf("invalid codec uuid=%v, %v", codec.UUID, res.UUID)
		}
		if codec.Audio.CodecName != "aac" || codec.Audio.Channels != 2 || codec.Audio.SampleRate != "44100" {
			return errors.Errorf("invalid codec audio=%v", codec.Audio)
		}
		if codec.Video.CodecName != "h264" || codec.Video.Profile != "High" || codec.Video.Width != 768 || codec.Video.Height != 320 {
			return errors.Errorf("invalid codec video=%v", codec.Video)
		}
		return nil
	}(); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive source failed")
		return
	}

	// Start virtual live streaming.
	type VLiveConfig struct {
		Platform string      `json:"platform"`
		Server   string      `json:"server"`
		Secret   string      `json:"secret"`
		Enabled  bool        `json:"enabled"`
		Custom   bool        `json:"custom"`
		Label    string      `json:"label"`
		Files    interface{} `json:"files"`
		Action   string      `json:"action"`
	}
	conf := make(map[string]*VLiveConfig)
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/secret", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive secret failed")
		return
	}

	bilibili, ok := conf["bilibili"]
	if !ok || bilibili == nil {
		r0 = errors.Errorf("invalid bilibili secret")
		return
	}

	// Restore the state of enabled.
	backup := *bilibili
	defer func() {
		backup.Action = "update"
		logger.Tf(ctx, "restore config %v", backup)

		if backup.Server == "" {
			backup.Server = bilibili.Server
		}
		if backup.Secret == "" {
			backup.Secret = bilibili.Secret
		}

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/secret", backup, nil)
	}()

	publishStreamID := fmt.Sprintf("publish-stream-%v-%v", os.Getpid(), rand.Int())
	bilibili.Secret = fmt.Sprintf("%v?secret=%v", publishStreamID, pubSecret)
	bilibili.Server = "rtmp://localhost/live/"
	bilibili.Enabled = true
	bilibili.Action = "update"
	bilibili.Custom = true
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/secret", &bilibili, nil); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive secret failed")
		return
	}

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", publishStreamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, publishStreamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}
}

func TestScenario_WithStream_PublishVLiveServerFile(t *testing.T) {
	tc(t, "虛擬直播：以上傳到伺服器的檔案為來源開播")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Copy virtual live source file to /data/upload and platform/containers/data/upload
	destDirs := []string{
		"/data/upload/",
		"platform/containers/data/upload",
		"../platform/containers/data/upload",
	}
	if err := copyToDest(ctx, *srsInputFile, destDirs...); err != nil {
		r0 = errors.Wrapf(err, "copy %v to %v", *srsInputFile, destDirs)
		return
	}

	// Get first matched source file.
	sourceFile, err := getExistsFile(ctx, filepath.Base(*srsInputFile), destDirs...)
	if err != nil {
		r0 = errors.Wrapf(err, "locate input file")
		return
	}
	if sourceFile == "" {
		r0 = errors.Errorf("no source file found")
		return
	}

	// If not absolution path, always use short path in upload.
	if !strings.HasPrefix(sourceFile, "/data/upload/") {
		sourceFile = "upload/" + path.Base(sourceFile)
	}

	// Use the file as uploaded file.
	res := struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Target string `json:"target"`
		UUID   string `json:"uuid"`
		Type   string `json:"type"`
	}{}
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/server", &struct {
		StreamFile string `json:"file"`
	}{
		StreamFile: sourceFile,
	}, &res); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive server failed")
		return
	}

	// Use the file as source file.
	codec := struct {
		UUID  string `json:"uuid"`
		Audio struct {
			CodecName  string `json:"codec_name"`
			Channels   int    `json:"channels"`
			SampleRate string `json:"sample_rate"`
		} `json:"audio"`
		Video struct {
			CodecName string `json:"codec_name"`
			Profile   string `json:"profile"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"video"`
	}{}
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/source", &struct {
		Platform string        `json:"platform"`
		Files    []interface{} `json:"files"`
	}{
		Platform: "bilibili",
		Files:    []interface{}{res},
	}, &struct {
		Files []interface{} `json:"files"`
	}{
		Files: []interface{}{&codec},
	}); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive source failed")
		return
	}

	if err := func() error {
		if codec.UUID != res.UUID {
			return errors.Errorf("invalid codec uuid=%v, %v", codec.UUID, res.UUID)
		}
		if codec.Audio.CodecName != "aac" || codec.Audio.Channels != 2 || codec.Audio.SampleRate != "44100" {
			return errors.Errorf("invalid codec audio=%v", codec.Audio)
		}
		if codec.Video.CodecName != "h264" || codec.Video.Profile != "High" || codec.Video.Width != 768 || codec.Video.Height != 320 {
			return errors.Errorf("invalid codec video=%v", codec.Video)
		}
		return nil
	}(); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive source failed")
		return
	}

	// Start virtual live streaming.
	type VLiveConfig struct {
		Platform string      `json:"platform"`
		Server   string      `json:"server"`
		Secret   string      `json:"secret"`
		Enabled  bool        `json:"enabled"`
		Custom   bool        `json:"custom"`
		Label    string      `json:"label"`
		Files    interface{} `json:"files"`
		Action   string      `json:"action"`
	}
	conf := make(map[string]*VLiveConfig)
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/secret", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive secret failed")
		return
	}

	bilibili, ok := conf["bilibili"]
	if !ok || bilibili == nil {
		r0 = errors.Errorf("invalid bilibili secret")
		return
	}

	// Restore the state of enabled.
	backup := *bilibili
	defer func() {
		backup.Action = "update"
		logger.Tf(ctx, "restore config %v", backup)

		if backup.Server == "" {
			backup.Server = bilibili.Server
		}
		if backup.Secret == "" {
			backup.Secret = bilibili.Secret
		}

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/secret", backup, nil)
	}()

	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	bilibili.Secret = fmt.Sprintf("%v?secret=%v", streamID, pubSecret)
	bilibili.Server = "rtmp://localhost/live/"
	bilibili.Enabled = true
	bilibili.Action = "update"
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/vlive/secret", &bilibili, nil); err != nil {
		r0 = errors.Wrapf(err, "request ffmpeg vlive secret failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", streamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, streamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}
}

func TestScenario_WithStream_PublishRtmpRecordMp4(t *testing.T) {
	tc(t, "錄製：推流觸發錄製，停止後驗證 MP4 檔案內容與規格")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsLongTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	backup := make(map[string]interface{})
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/query", nil, &backup); err != nil {
		r0 = errors.Wrapf(err, "request record query failed")
		return
	}
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", backup, nil)
	}()

	// Enable the record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{true}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Wait for record to save file.
	select {
	case <-ctx.Done():
	case <-time.After(25 * time.Second):
	}

	// Stop record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{false}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}
	logger.Tf(ctx, "stop record worker done")

	// Query the record file.
	type RecordFile struct {
		Stream   string  `json:"stream"`
		UUID     string  `json:"uuid"`
		Duration float64 `json:"duration"`
		Progress bool    `json:"progress"`
	}
	var recordFile *RecordFile
	defer func() {
		if recordFile == nil || recordFile.UUID == "" {
			return
		}
		logger.Tf(ctx, "remove record file %v", recordFile)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/remove", &struct {
			UUID string `json:"uuid"`
		}{recordFile.UUID}, nil)
	}()
	defer cancel()

	for i := 0; i < 60; i++ {
		files := []RecordFile{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/files", nil, &files); err != nil {
			r0 = errors.Wrapf(err, "request record files failed")
			return
		}

		for _, file := range files {
			if file.Stream == streamID {
				recordFile = &file
				break
			}
		}

		if recordFile == nil || recordFile.Progress {
			select {
			case <-ctx.Done():
				r0 = errors.Wrapf(ctx.Err(), "record file not found")
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		break
	}

	if recordFile == nil {
		r0 = errors.Errorf("record file not found")
		return
	}
	if recordFile.Progress {
		r0 = errors.Errorf("record file is progress, %v", recordFile)
		return
	}
	if recordFile.Duration < 10 {
		r0 = errors.Errorf("record file duration too short, %v", recordFile)
		return
	}

	time.Sleep(3 * time.Second)
	logger.Tf(ctx, "record ok, file is %v", recordFile)
	cancel()
}

func TestScenario_WithStream_PostProcessCpFile_RecordMp4(t *testing.T) {
	tc(t, "錄製後置處理：複製 MP4 檔案的流程")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsLongTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type RecordConfig struct {
		All  bool   `json:"all"`
		Home string `json:"home"`
		// Post processing.
		ProcessCpDir string `json:"processCpDir"`
	}
	type RecordPostProcess struct {
		PostProcess string `json:"postProcess"`
		PostCpDir   string `json:"postCpDir"`
	}

	var backup RecordConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/query", nil, &backup); err != nil {
		r0 = errors.Wrapf(err, "request record query failed")
		return
	}
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", backup, nil)
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/post-processing", &RecordPostProcess{
			PostProcess: "post-cp-file",
			PostCpDir:   backup.ProcessCpDir,
		}, nil)
	}()

	// Change the dir to new dir.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/post-processing", &RecordPostProcess{
		PostProcess: "post-cp-file",
		PostCpDir:   "./containers/data/srs-s3-bucket",
	}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	// Enable the record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{true}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Wait for record to save file.
	select {
	case <-ctx.Done():
	case <-time.After(25 * time.Second):
	}

	// Stop record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{false}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}
	logger.Tf(ctx, "stop record worker done")

	// Query the record file.
	type RecordFile struct {
		Stream   string  `json:"stream"`
		UUID     string  `json:"uuid"`
		Duration float64 `json:"duration"`
		Progress bool    `json:"progress"`
	}
	var recordFile *RecordFile
	defer func() {
		if recordFile == nil || recordFile.UUID == "" {
			return
		}
		logger.Tf(ctx, "remove record file %v", recordFile)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/remove", &struct {
			UUID string `json:"uuid"`
		}{recordFile.UUID}, nil)
	}()
	// Try to find the file in relative dir, then find file in absolute path.
	buildRecordCpFilePath := func(_ *RecordFile) string {
		filename := fmt.Sprintf("%v.mp4", recordFile.UUID)
		p := path.Join("../platform/containers/data/srs-s3-bucket", filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return path.Join("/data/srs-s3-bucket", filename)
	}
	defer func() {
		// Remove the cp file at tmp.
		if recordFile != nil && recordFile.UUID != "" {
			if _, err := os.Stat(buildRecordCpFilePath(recordFile)); err == nil {
				os.Remove(buildRecordCpFilePath(recordFile))
			}
		}
	}()
	defer cancel()

	for i := 0; i < 60; i++ {
		files := []RecordFile{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/files", nil, &files); err != nil {
			r0 = errors.Wrapf(err, "request record files failed")
			return
		}

		for _, file := range files {
			if file.Stream == streamID {
				recordFile = &file
				break
			}
		}

		if recordFile == nil || recordFile.Progress {
			select {
			case <-ctx.Done():
				r0 = errors.Wrapf(ctx.Err(), "record file not found")
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		break
	}

	if recordFile == nil {
		r0 = errors.Errorf("record file not found")
		return
	}
	if recordFile.Progress {
		r0 = errors.Errorf("record file is progress, %v", recordFile)
		return
	}
	if recordFile.Duration < 10 {
		r0 = errors.Errorf("record file duration too short, %v", recordFile)
		return
	}
	if _, err := os.Stat(buildRecordCpFilePath(recordFile)); err != nil {
		r0 = errors.Errorf("record file not found, %v", buildRecordCpFilePath(recordFile))
		return
	}

	time.Sleep(3 * time.Second)
	logger.Tf(ctx, "record ok, file is %v", recordFile)
	cancel()
}

func TestScenario_WithStream_Publishing_EndRecordMp4(t *testing.T) {
	tc(t, "錄製：推流中途中斷錄製，驗證已產生 MP4 的完整性")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsLongTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	backup := make(map[string]interface{})
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/query", nil, &backup); err != nil {
		r0 = errors.Wrapf(err, "request record query failed")
		return
	}
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", backup, nil)
	}()

	// Enable the record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{true}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Wait for record to save file.
	select {
	case <-ctx.Done():
	case <-time.After(20 * time.Second):
	}

	// Query the recording task.
	type RecordFile struct {
		Stream   string  `json:"stream"`
		UUID     string  `json:"uuid"`
		Duration float64 `json:"duration"`
		Progress bool    `json:"progress"`
	}
	queryRecordFile := func(uuid, streamID string) *RecordFile {
		files := []RecordFile{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/files", nil, &files); err != nil {
			r0 = errors.Wrapf(err, "request record files failed")
			return nil
		}

		for _, file := range files {
			if uuid != "" {
				if file.UUID == uuid {
					return &file
				}
			} else {
				if file.Stream == streamID {
					return &file
				}
			}
		}
		return nil
	}
	removeFile := func(f *RecordFile) {
		if f == nil || f.UUID == "" {
			return
		}
		logger.Tf(ctx, "remove record file %v", f)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/remove", &struct {
			UUID string `json:"uuid"`
		}{f.UUID}, nil)
	}

	// Quickly end the record task.
	if true {
		endedRecordFile := queryRecordFile("", streamID)
		if endedRecordFile == nil {
			r0 = errors.Errorf("record file not found")
			return
		}
		defer func() {
			removeFile(endedRecordFile)
		}()

		// End recording task.
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/end", &struct {
			UUID string `json:"uuid"`
		}{endedRecordFile.UUID}, nil); err != nil {
			r0 = errors.Wrapf(err, "request ending record task failed")
			return
		}
		logger.Tf(ctx, "end record task done")

		// For quickly ending record task, should be ended very soon.
		for i := 0; i < 5; i++ {
			if endedRecordFile = queryRecordFile(endedRecordFile.UUID, ""); endedRecordFile == nil || endedRecordFile.Progress {
				select {
				case <-ctx.Done():
					r0 = errors.Wrapf(ctx.Err(), "record file not found")
					return
				case <-time.After(1 * time.Second):
					continue
				}
			}
			break
		}

		// Remove the ended record file now, because there will be new task generated.
		removeFile(endedRecordFile)
		endedRecordFile = nil
	}

	// There should be another record file, after current task is ended.
	select {
	case <-ctx.Done():
	case <-time.After(20 * time.Second):
	}

	// Stop record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{false}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}
	logger.Tf(ctx, "stop record worker done")

	// Query the record file.
	var recordFile *RecordFile
	defer func() {
		removeFile(recordFile)
	}()
	defer cancel()

	for i := 0; i < 60; i++ {
		if recordFile = queryRecordFile("", streamID); recordFile == nil || recordFile.Progress {
			select {
			case <-ctx.Done():
				r0 = errors.Wrapf(ctx.Err(), "record file not found")
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		break
	}

	if recordFile == nil {
		r0 = errors.Errorf("record file not found")
		return
	}
	if recordFile.Progress {
		r0 = errors.Errorf("record file is progress, %v", recordFile)
		return
	}
	if recordFile.Duration < 10 {
		r0 = errors.Errorf("record file duration too short, %v", recordFile)
		return
	}

	time.Sleep(3 * time.Second)
	logger.Tf(ctx, "record ok, file is %v", recordFile)
	cancel()
}

func TestScenario_WithStream_Unpublished_EndRecordMp4(t *testing.T) {
	tc(t, "錄製邊界：未推流就結束錄製的處理")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsLongTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	backup := make(map[string]interface{})
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/query", nil, &backup); err != nil {
		r0 = errors.Wrapf(err, "request record query failed")
		return
	}
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", backup, nil)
	}()

	// Enable the record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{true}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})

	ffmpegCtx, ffmpegCancel := context.WithCancel(ctx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ffmpegCtx, cancel)
	}()
	defer ffmpegCancel()

	// Wait for record to save file.
	select {
	case <-ctx.Done():
	case <-time.After(20 * time.Second):
	}

	// Query the recording task.
	type RecordFile struct {
		Stream   string  `json:"stream"`
		UUID     string  `json:"uuid"`
		Duration float64 `json:"duration"`
		Progress bool    `json:"progress"`
	}
	queryRecordFile := func(uuid, streamID string) *RecordFile {
		files := []RecordFile{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/files", nil, &files); err != nil {
			r0 = errors.Wrapf(err, "request record files failed")
			return nil
		}

		for _, file := range files {
			if uuid != "" {
				if file.UUID == uuid {
					return &file
				}
			} else {
				if file.Stream == streamID {
					return &file
				}
			}
		}
		return nil
	}
	removeFile := func(f *RecordFile) {
		if f == nil || f.UUID == "" {
			return
		}
		logger.Tf(ctx, "remove record file %v", f)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/remove", &struct {
			UUID string `json:"uuid"`
		}{f.UUID}, nil)
	}

	// Quickly end the record task.
	if true {
		endedRecordFile := queryRecordFile("", streamID)
		if endedRecordFile == nil {
			r0 = errors.Errorf("record file not found")
			return
		}
		defer func() {
			removeFile(endedRecordFile)
		}()

		// Unpublish the ffmpeg stream.
		ffmpegCancel()
		select {
		case <-ctx.Done():
		case <-time.After(1 * time.Second):
		}

		// End recording task.
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/end", &struct {
			UUID string `json:"uuid"`
		}{endedRecordFile.UUID}, nil); err != nil {
			r0 = errors.Wrapf(err, "request ending record task failed")
			return
		}
		logger.Tf(ctx, "end record task done")

		// For quickly ending record task, should be ended very soon.
		for i := 0; i < 5; i++ {
			if endedRecordFile = queryRecordFile(endedRecordFile.UUID, ""); endedRecordFile == nil || endedRecordFile.Progress {
				select {
				case <-ctx.Done():
					r0 = errors.Wrapf(ctx.Err(), "record file not found")
					return
				case <-time.After(1 * time.Second):
					continue
				}
			}
			break
		}

		// Remove the ended record file now, because there will be new task generated.
		removeFile(endedRecordFile)
		endedRecordFile = nil
	}

	// There should never be another record file, after current task is ended.
	select {
	case <-ctx.Done():
	case <-time.After(20 * time.Second):
	}

	// Stop record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{false}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}
	logger.Tf(ctx, "stop record worker done")

	// Query the record file.
	var recordFile *RecordFile
	defer func() {
		removeFile(recordFile)
	}()
	defer cancel()

	for i := 0; i < 35; i++ {
		if recordFile = queryRecordFile("", streamID); recordFile == nil {
			select {
			case <-ctx.Done():
				r0 = errors.Wrapf(ctx.Err(), "record file not found")
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		break
	}

	if recordFile != nil {
		r0 = errors.Errorf("record file found, %v", recordFile)
		return
	}

	time.Sleep(3 * time.Second)
	logger.Tf(ctx, "record ok, file is %v", recordFile)
	cancel()
}

func TestScenario_WithStream_RecordWithGlobFiltersAllowed(t *testing.T) {
	tc(t, "錄製過濾：符合 glob 白名單的串流會被錄製")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsLongTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type RecordConfig struct {
		All   bool     `json:"all"`
		Home  string   `json:"home"`
		Globs []string `json:"globs"`
	}
	var backup RecordConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/query", nil, &backup); err != nil {
		r0 = errors.Wrapf(err, "request record query failed")
		return
	}
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", backup, nil)
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/globs", backup, nil)
	}()

	// Enable the record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{true}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	// Setup the glob filtes.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/globs", &struct {
		Globs []string `json:"globs"`
	}{
		Globs: []string{"/*/*"},
	}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Wait for record to save file.
	select {
	case <-ctx.Done():
	case <-time.After(25 * time.Second):
	}

	// Stop record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{false}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}
	logger.Tf(ctx, "stop record worker done")

	// Query the record file.
	type RecordFile struct {
		Stream   string  `json:"stream"`
		UUID     string  `json:"uuid"`
		Duration float64 `json:"duration"`
		Progress bool    `json:"progress"`
	}
	var recordFile *RecordFile
	defer func() {
		if recordFile == nil || recordFile.UUID == "" {
			return
		}
		logger.Tf(ctx, "remove record file %v", recordFile)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/remove", &struct {
			UUID string `json:"uuid"`
		}{recordFile.UUID}, nil)
	}()
	defer cancel()

	for i := 0; i < 60; i++ {
		files := []RecordFile{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/files", nil, &files); err != nil {
			r0 = errors.Wrapf(err, "request record files failed")
			return
		}

		for _, file := range files {
			if file.Stream == streamID {
				recordFile = &file
				break
			}
		}

		if recordFile == nil || recordFile.Progress {
			select {
			case <-ctx.Done():
				r0 = errors.Wrapf(ctx.Err(), "record file not found")
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		break
	}

	if recordFile == nil {
		r0 = errors.Errorf("record file not found")
		return
	}
	if recordFile.Progress {
		r0 = errors.Errorf("record file is progress, %v", recordFile)
		return
	}
	if recordFile.Duration < 10 {
		r0 = errors.Errorf("record file duration too short, %v", recordFile)
		return
	}

	time.Sleep(3 * time.Second)
	logger.Tf(ctx, "record ok, file is %v", recordFile)
	cancel()
}

func TestScenario_WithStream_RecordWithGlobFiltersDenied(t *testing.T) {
	tc(t, "錄製過濾：不符合 glob 規則的串流被拒絕錄製")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsLongTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type RecordConfig struct {
		All   bool     `json:"all"`
		Home  string   `json:"home"`
		Globs []string `json:"globs"`
	}
	var backup RecordConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/query", nil, &backup); err != nil {
		r0 = errors.Wrapf(err, "request record query failed")
		return
	}
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", backup, nil)
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/globs", backup, nil)
	}()

	// Enable the record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{true}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	// Setup the glob filtes.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/globs", &struct {
		Globs []string `json:"globs"`
	}{
		Globs: []string{"/match/nothing"},
	}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Wait for record to save file.
	select {
	case <-ctx.Done():
	case <-time.After(25 * time.Second):
	}

	// Stop record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{false}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}
	logger.Tf(ctx, "stop record worker done")

	// Query the record file.
	type RecordFile struct {
		Stream   string  `json:"stream"`
		UUID     string  `json:"uuid"`
		Duration float64 `json:"duration"`
		Progress bool    `json:"progress"`
	}
	var recordFile *RecordFile
	defer func() {
		if recordFile == nil || recordFile.UUID == "" {
			return
		}
		logger.Tf(ctx, "remove record file %v", recordFile)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/remove", &struct {
			UUID string `json:"uuid"`
		}{recordFile.UUID}, nil)
	}()
	defer cancel()

	for i := 0; i < 10; i++ {
		files := []RecordFile{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/files", nil, &files); err != nil {
			r0 = errors.Wrapf(err, "request record files failed")
			return
		}

		for _, file := range files {
			if file.Stream == streamID {
				recordFile = &file
				break
			}
		}

		if recordFile == nil || recordFile.Progress {
			select {
			case <-ctx.Done():
				r0 = errors.Wrapf(ctx.Err(), "record file not found")
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		break
	}

	// Because we use a glob filter that not match any stream, so the record file should not be found.
	if recordFile != nil {
		r0 = errors.Errorf("record file should not found")
		return
	}

	// Now cancel the FFmepg, then wait for a while. We should never wait then cancel, because if we reset
	// the glob filter first, and it will make the record the last piece of ts files.
	cancel()
	time.Sleep(5 * time.Second)
	logger.Tf(ctx, "record ok, file is %v", recordFile)
}

func TestScenario_WithStream_PublishRtmpForwardPlatform(t *testing.T) {
	tc(t, "轉推：推流後 forward 到目標平台並驗證輸出可播放")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type ForwardConfig struct {
		Action   string `json:"action"`
		Enabled  bool   `json:"enabled"`
		Custom   bool   `json:"custom"`
		Label    string `json:"label"`
		Platform string `json:"platform"`
		Secret   string `json:"secret"`
		Server   string `json:"server"`
	}
	conf := make(map[string]*ForwardConfig)
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/secret", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request forward query failed")
		return
	}

	// Use a dedicated TEST target instead of mutating the user's built-in platform config, so the
	// test is isolated (and always starts from a clean state).
	forwardStreamID := fmt.Sprintf("forward-stream-%v-%v", os.Getpid(), rand.Int())
	testPlatform := fmt.Sprintf("forwarding-ftest-%v-%v", os.Getpid(), rand.Int())
	testTarget := &ForwardConfig{
		Enabled:  false,
		Custom:   true,
		Label:    "TEST forward platform",
		Platform: testPlatform,
		Secret:   fmt.Sprintf("%v?secret=%v", forwardStreamID, pubSecret),
		Server:   "rtmp://localhost/live/",
	}

	// Delete the dedicated target when the case finishes.
	defer func() {
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/secret", map[string]interface{}{
			"action": "delete", "platform": testPlatform,
		}, nil)
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	defer cancel()
	select {
	case <-ctx.Done():
		return
	case <-ffmpeg.ReadyCtx().Done():
	}

	// Enable the forward worker.
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
	}

	testTarget.Secret = fmt.Sprintf("%v?secret=%v", forwardStreamID, pubSecret)
	testTarget.Server = "rtmp://localhost/live/"
	testTarget.Enabled = true
	testTarget.Action = "update"
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/secret", testTarget, nil); err != nil {
		r0 = errors.Wrapf(err, "request forward apply failed")
		return
	}

	// Verify the dedicated target was actually persisted, and print its state to stdout.
	{
		var streams []map[string]interface{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/streams", map[string]string{"action": "query"}, &streams); err != nil {
			fmt.Printf("[apply-diag] query failed: %v\n", err)
		} else {
			fmt.Printf("[apply-diag] %v targets:\n", len(streams))
			for _, s := range streams {
				fmt.Printf("  platform=%v status=%v enabled=%v restarts=%v lastError=%v\n",
					s["platform"], s["status"], s["enabled"], s["restartCount"], s["lastError"])
			}
		}
	}

	// Wait until the target reports running through the platform API (the public contract), not by
	// reading Redis internals: the target only publishes its output once the program is bound and
	// the fanout has started. Recording earlier reads a not-yet-published HTTP-FLV and exits.
	for i := 0; i < 20; i++ {
		var streams []map[string]interface{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/streams", map[string]string{"action": "query"}, &streams); err == nil {
			for _, s := range streams {
				if fmt.Sprint(s["platform"]) == testPlatform && fmt.Sprint(s["status"]) == "running" {
					logger.Tf(ctx, "forward target %v is running", testPlatform)
					i = 99
					break
				}
			}
		}
		if i < 20 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", forwardStreamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, forwardStreamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		// Dump every forward target state so a short output can be explained.
		var streams []map[string]interface{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/streams", map[string]string{"action": "query"}, &streams); err == nil {
			f, _ := os.Create("/tmp/forward-state.txt")
			if f != nil {
				fmt.Fprintf(f, "forward targets at failure (%v):\n", len(streams))
				for _, s := range streams {
					fmt.Fprintf(f, "platform=%v status=%v restarts=%v lastError=%v events=%v\n",
						s["platform"], s["status"], s["restartCount"], s["lastError"], s["events"])
				}
				f.Close()
			}
		}
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}
}

// testForwardKeepalive verifies that a source outage does not interrupt the downstream publish.
// With failover="" (hold) the downstream holds the last frame; with failover="slate" the program
// switches to a fallback slate. In both cases the fanout must not restart.
func testForwardKeepalive(t *testing.T, failover string, expectSlate bool) {
	desc := "轉推：來源中斷時下游不斷線（hold last frame）"
	if expectSlate {
		desc = "轉推：來源中斷時切換備用畫面（slate）且下游不斷線"
	}
	tc(t, desc)
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{Publish: &pubSecret}); err != nil {
		r0 = err
		return
	}

	type ForwardConfig struct {
		Action   string `json:"action"`
		Enabled  bool   `json:"enabled"`
		Custom   bool   `json:"custom"`
		Label    string `json:"label"`
		Platform string `json:"platform"`
		Secret   string `json:"secret"`
		Server   string `json:"server"`
		Failover string `json:"failover"`
	}
	conf := make(map[string]*ForwardConfig)
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/secret", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request forward query failed")
		return
	}

	forwardStreamID := fmt.Sprintf("forward-keepalive-%v-%v", os.Getpid(), rand.Int())
	bilibili, ok := conf["bilibili"]
	if !ok || bilibili == nil {
		bilibili = &ForwardConfig{
			Enabled:  false,
			Custom:   true,
			Label:    "Test",
			Platform: "bilibili",
			Server:   "rtmp://localhost/live/",
		}
		conf["bilibili"] = bilibili
	}

	// Restore the state of forward.
	backup := *bilibili
	defer func() {
		backup.Action = "update"
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/secret", backup, nil)
	}()

	var wg sync.WaitGroup
	defer wg.Wait()
	// Must be after wg.Wait, so it runs before it and stops the source FFmpeg.
	defer cancel()

	streamID := fmt.Sprintf("stream-keepalive-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)

	// Publish the source for a while, then it exits to simulate an outage.
	runSource := func(duration time.Duration) {
		ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
			v.args = []string{
				"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
				"-f", "flv", streamURL,
			}
			// Do not cancel the case when the source quits, we simulate an outage.
			v.cancelCaseWhenQuit = false
			v.ffmpegDuration = duration
		})
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ffmpeg.Run(ctx, cancel)
		}()
	}

	forwardStatus := func() map[string]interface{} {
		var streams []map[string]interface{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/streams", nil, &streams); err != nil {
			return nil
		}
		for _, m := range streams {
			if m["platform"] == "bilibili" {
				return m
			}
		}
		return nil
	}

	waitStatus := func(desc string, pfn func(m map[string]interface{}) bool) bool {
		for i := 0; i < 30; i++ {
			if m := forwardStatus(); m != nil && pfn(m) {
				logger.Tf(ctx, "forward status matched %v, %v", desc, m)
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Second):
			}
		}
		return false
	}

	// Start the source and wait it to be active.
	runSource(9 * time.Second)
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Second):
	}

	// Enable the forward to the local target.
	bilibili.Secret = fmt.Sprintf("%v?secret=%v", forwardStreamID, pubSecret)
	bilibili.Server = "rtmp://localhost/live/"
	bilibili.Enabled = true
	bilibili.Action = "update"
	bilibili.Failover = failover
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/secret", bilibili, nil); err != nil {
		r0 = errors.Wrapf(err, "request forward apply failed")
		return
	}

	// Verify the dedicated target was actually persisted, and print its state to stdout.
	{
		var streams []map[string]interface{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/forward/streams", map[string]string{"action": "query"}, &streams); err != nil {
			fmt.Printf("[apply-diag] query failed: %v\n", err)
		} else {
			fmt.Printf("[apply-diag] %v targets:\n", len(streams))
			for _, s := range streams {
				fmt.Printf("  platform=%v status=%v enabled=%v restarts=%v lastError=%v\n",
					s["platform"], s["status"], s["enabled"], s["restartCount"], s["lastError"])
			}
		}
	}

	// The program should be live, and the fanout running.
	if !waitStatus("live", func(m map[string]interface{}) bool {
		return m["status"] == "running" && forwardLiveMode(m["programMode"]) && m["fallback"] == false
	}) {
		r1 = errors.Errorf("forward not live, status=%v", forwardStatus())
		return
	}
	// The local output metrics must be reported while running.
	if !waitStatus("metrics", func(m map[string]interface{}) bool {
		metrics, ok := m["metrics"].(map[string]interface{})
		if !ok {
			return false
		}
		frame, _ := metrics["frame"].(float64)
		sec, _ := metrics["mediaSeconds"].(float64)
		return frame > 0 && sec > 0
	}) {
		r1 = errors.Errorf("missing forward metrics, status=%v", forwardStatus())
		return
	}
	// The diagnostics timeline must record the run start and the first output.
	if !waitStatus("events", func(m map[string]interface{}) bool {
		evs, ok := m["events"].([]interface{})
		if !ok {
			return false
		}
		names := map[string]bool{}
		for _, e := range evs {
			if em, ok := e.(map[string]interface{}); ok {
				if name, ok := em["name"].(string); ok {
					names[name] = true
				}
			}
		}
		return names["start"] && names["ready"]
	}) {
		r1 = errors.Errorf("missing forward events, status=%v", forwardStatus())
		return
	}
	// Record the restart count once the fanout is running, to assert the outage causes no restart.
	baseRestart := forwardStatus()["restartCount"].(float64)

	// The source exits now. The program must keep the downstream alive: hold the last frame
	// (hold mode) or switch to the fallback slate (slate mode). The fanout must not restart.
	// The composite failover (slate) renders the base+source overlay itself, so it keeps the
	// "composite" mode and only flips fallback=true; the hold relay stays "live".
	expectMode := "live"
	if expectSlate {
		expectMode = "composite"
	}
	if !waitStatus("fallback", func(m map[string]interface{}) bool {
		return m["status"] == "running" && m["programMode"] == expectMode && m["fallback"] == true
	}) {
		r2 = errors.Errorf("forward not fallback, status=%v", forwardStatus())
		return
	}
	// In hold mode the fanout must not have restarted during the outage (it freezes on the last
	// frame), which proves the downstream never dropped. Slate mode restarts the fanout to show
	// the slate (FFmpeg copy cannot switch inputs), so this is asserted only for hold.
	if !expectSlate {
		if m := forwardStatus(); m == nil || m["restartCount"].(float64) != baseRestart {
			r3 = errors.Errorf("downstream restarted during outage, status=%v", m)
			return
		}
	}

	// Bring the source back, the program bus must switch back to live.
	runSource(20 * time.Second)
	if !waitStatus("live again", func(m map[string]interface{}) bool {
		return m["status"] == "running" && forwardLiveMode(m["programMode"]) && m["fallback"] == false
	}) {
		r4 = errors.Errorf("forward not recovered, status=%v", forwardStatus())
		return
	}
	// The output must actually resume (frames flow), not stay frozen on the last frame. This
	// covers the bug where FFmpeg's HTTP-FLV reconnect never recovers after the stream returns.
	if !waitStatus("output resumed", func(m map[string]interface{}) bool {
		metrics, ok := m["metrics"].(map[string]interface{})
		if !ok {
			return false
		}
		frame, _ := metrics["frame"].(float64)
		noOutput, _ := metrics["noOutputSeconds"].(float64)
		return frame > 0 && noOutput < 3
	}) {
		r4 = errors.Errorf("forward did not resume output, status=%v", forwardStatus())
		return
	}
}

// forwardLiveMode reports whether the program is showing the live source: the hold relay
// ("live") or the composite ("composite", which renders the source itself and falls back to the
// slate base).
func forwardLiveMode(v interface{}) bool {
	return v == "live" || v == "composite"
}

func TestScenario_WithStream_PublishRtmpForwardKeepalive(t *testing.T) {
	testForwardKeepalive(t, "", false)
}

func TestScenario_WithStream_PublishRtmpForwardSlate(t *testing.T) {
	testForwardKeepalive(t, "slate", true)
}

func TestScenario_WithStream_PublishRtmpTranscodeDefault(t *testing.T) {
	tc(t, "轉碼：預設參數（profile/preset）輸出驗證")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5, r6 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5, r6); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type TranscodeConfig struct {
		All           bool   `json:"all"`
		VideoCodec    string `json:"vcodec"`
		VideoProfile  string `json:"vprofile"`
		VideoPreset   string `json:"vpreset"`
		VideoBitrate  int    `json:"vbitrate"`
		AudioCodec    string `json:"acodec"`
		AudioBitrate  int    `json:"abitrate"`
		AudioChannels int    `json:"achannels"`
		Server        string `json:"server"`
		Secret        string `json:"secret"`
	}
	var conf TranscodeConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/query", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request transcode query failed")
		return
	}

	// Restore the state of transcode.
	backup := conf
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", backup, nil)
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	defer cancel()
	select {
	case <-ctx.Done():
		return
	case <-ffmpeg.ReadyCtx().Done():
	}

	// Enable the transcode worker.
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
	}

	transcodeStreamID := fmt.Sprintf("transcoded-stream-%v-%v", os.Getpid(), rand.Int())
	conf.All = true
	conf.Server = "rtmp://localhost/live/"
	conf.Secret = fmt.Sprintf("%v?secret=%v", transcodeStreamID, pubSecret)
	conf.VideoCodec = "libx264"
	conf.VideoBitrate = 200
	conf.VideoProfile = "baseline"
	conf.VideoPreset = "ultrafast"
	conf.AudioCodec = "aac"
	conf.AudioBitrate = 16
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", &conf, nil); err != nil {
		r0 = errors.Wrapf(err, "request transcode apply failed")
		return
	}

	// Check the transcode task.
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
	}

	task := struct {
		UUID    string `json:"uuid"`
		Enabled bool   `json:"enabled"`
		Input   string `json:"input"`
		Output  string `json:"output"`
		Frame   struct {
			Log    string `json:"log"`
			Update string `json:"update"`
		} `json:"frame"`
	}{}
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/task", nil, &task); err != nil {
		r0 = errors.Wrapf(err, "request transcode query failed")
		return
	}
	if !task.Enabled || task.Input == "" || task.Output == "" || task.Frame.Log == "" || task.Frame.Update == "" ||
		task.UUID == "" {
		r0 = errors.Errorf("invalid task=%v", task)
		return
	}

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", transcodeStreamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, transcodeStreamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}

	if m.Audio().Channels != 2 {
		r6 = errors.Errorf("invalid audio channels=%v, %v, %v", m.Audio().Channels, m.String(), str)
	}
}

func TestScenario_WithStream_PublishRtmpTranscodeFollow(t *testing.T) {
	tc(t, "轉碼：follow 模式（跟隨來源解析度）輸出驗證")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5, r6 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5, r6); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type TranscodeConfig struct {
		All           bool   `json:"all"`
		VideoCodec    string `json:"vcodec"`
		VideoProfile  string `json:"vprofile"`
		VideoPreset   string `json:"vpreset"`
		VideoBitrate  int    `json:"vbitrate"`
		AudioCodec    string `json:"acodec"`
		AudioBitrate  int    `json:"abitrate"`
		AudioChannels int    `json:"achannels"`
		Server        string `json:"server"`
		Secret        string `json:"secret"`
	}
	var conf TranscodeConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/query", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request transcode query failed")
		return
	}

	// Restore the state of transcode.
	backup := conf
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", backup, nil)
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	defer cancel()
	select {
	case <-ctx.Done():
		return
	case <-ffmpeg.ReadyCtx().Done():
	}

	// Enable the transcode worker.
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
	}

	transcodeStreamID := fmt.Sprintf("transcoded-stream-%v-%v", os.Getpid(), rand.Int())
	conf.All = true
	conf.Server = "rtmp://localhost/live/"
	conf.Secret = fmt.Sprintf("%v?secret=%v", transcodeStreamID, pubSecret)
	conf.VideoCodec = "libx264"
	conf.VideoBitrate = 200
	conf.VideoProfile = "baseline"
	conf.VideoPreset = "ultrafast"
	conf.AudioCodec = "aac"
	conf.AudioBitrate = 16
	conf.AudioChannels = 0
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", &conf, nil); err != nil {
		r0 = errors.Wrapf(err, "request transcode apply failed")
		return
	}

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", transcodeStreamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, transcodeStreamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}

	if m.Audio().Channels != 2 {
		r6 = errors.Errorf("invalid audio channels=%v, %v, %v", m.Audio().Channels, m.String(), str)
	}
}

func TestScenario_WithStream_PublishRtmpTranscodeMono(t *testing.T) {
	tc(t, "轉碼：單聲道音訊輸出驗證")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5, r6 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5, r6); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type TranscodeConfig struct {
		All           bool   `json:"all"`
		VideoCodec    string `json:"vcodec"`
		VideoProfile  string `json:"vprofile"`
		VideoPreset   string `json:"vpreset"`
		VideoBitrate  int    `json:"vbitrate"`
		AudioCodec    string `json:"acodec"`
		AudioBitrate  int    `json:"abitrate"`
		AudioChannels int    `json:"achannels"`
		Server        string `json:"server"`
		Secret        string `json:"secret"`
	}
	var conf TranscodeConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/query", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request transcode query failed")
		return
	}

	// Restore the state of transcode.
	backup := conf
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", backup, nil)
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	defer cancel()
	select {
	case <-ctx.Done():
		return
	case <-ffmpeg.ReadyCtx().Done():
	}

	// Enable the transcode worker.
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
	}

	transcodeStreamID := fmt.Sprintf("transcoded-stream-%v-%v", os.Getpid(), rand.Int())
	conf.All = true
	conf.Server = "rtmp://localhost/live/"
	conf.Secret = fmt.Sprintf("%v?secret=%v", transcodeStreamID, pubSecret)
	conf.VideoCodec = "libx264"
	conf.VideoBitrate = 200
	conf.VideoProfile = "baseline"
	conf.VideoPreset = "ultrafast"
	conf.AudioCodec = "aac"
	conf.AudioBitrate = 16
	conf.AudioChannels = 1
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", &conf, nil); err != nil {
		r0 = errors.Wrapf(err, "request transcode apply failed")
		return
	}

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", transcodeStreamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, transcodeStreamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}

	if m.Audio().Channels != 1 {
		r6 = errors.Errorf("invalid audio channels=%v, %v, %v", m.Audio().Channels, m.String(), str)
	}
}

func TestScenario_WithStream_PublishRtmpTranscodeStereo(t *testing.T) {
	tc(t, "轉碼：立體聲音訊輸出驗證")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5, r6 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5, r6); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	// Query the old config.
	type TranscodeConfig struct {
		All           bool   `json:"all"`
		VideoCodec    string `json:"vcodec"`
		VideoProfile  string `json:"vprofile"`
		VideoPreset   string `json:"vpreset"`
		VideoBitrate  int    `json:"vbitrate"`
		AudioCodec    string `json:"acodec"`
		AudioBitrate  int    `json:"abitrate"`
		AudioChannels int    `json:"achannels"`
		Server        string `json:"server"`
		Secret        string `json:"secret"`
	}
	var conf TranscodeConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/query", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request transcode query failed")
		return
	}

	// Restore the state of transcode.
	backup := conf
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", backup, nil)
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	defer cancel()
	select {
	case <-ctx.Done():
		return
	case <-ffmpeg.ReadyCtx().Done():
	}

	// Enable the transcode worker.
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
	}

	transcodeStreamID := fmt.Sprintf("transcoded-stream-%v-%v", os.Getpid(), rand.Int())
	conf.All = true
	conf.Server = "rtmp://localhost/live/"
	conf.Secret = fmt.Sprintf("%v?secret=%v", transcodeStreamID, pubSecret)
	conf.VideoCodec = "libx264"
	conf.VideoBitrate = 200
	conf.VideoProfile = "baseline"
	conf.VideoPreset = "ultrafast"
	conf.AudioCodec = "aac"
	conf.AudioBitrate = 16
	conf.AudioChannels = 2
	if err := NewApi().WithAuth(ctx, "/terraform/v1/ffmpeg/transcode/apply", &conf, nil); err != nil {
		r0 = errors.Wrapf(err, "request transcode apply failed")
		return
	}

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", transcodeStreamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, transcodeStreamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}

	if m.Audio().Channels != 2 {
		r6 = errors.Errorf("invalid audio channels=%v, %v, %v", m.Audio().Channels, m.String(), str)
	}
}

func TestScenario_WithStream_CallbackOnPublishSuccess(t *testing.T) {
	tc(t, "回呼：on_publish 允許推流的場景")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	type CallbackConfig struct {
		All    bool   `json:"all"`
		Opaque string `json:"opaque"`
		Target string `json:"target"`
	}
	var conf CallbackConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/query", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request hooks apply failed")
		return
	}

	// Restore the state of transcode.
	backup := conf
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/apply", backup, nil)
	}()

	// Enable the callback worker.
	conf.All = true
	conf.Target = fmt.Sprintf("%v/terraform/v1/mgmt/hooks/example?fail=false", *endpoint)
	conf.Opaque = fmt.Sprintf("opaque-%v", rand.Int())
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/apply", &conf, nil); err != nil {
		r0 = errors.Wrapf(err, "request hooks apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", streamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, streamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	str, m := ffprobe.Result()
	if len(m.Streams) != 2 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}

	if ts := 90; m.Format.ProbeScore < ts {
		r4 = errors.Errorf("low score=%v < %v, %v, %v", m.Format.ProbeScore, ts, m.String(), str)
	}
	if dv := m.Duration(); dv < duration/5 {
		r5 = errors.Errorf("short duration=%v < %v, %v, %v", dv, duration, m.String(), str)
	}
}

func TestScenario_WithStream_CallbackOnPublishFailed(t *testing.T) {
	tc(t, "回呼：on_publish 拒絕推流的場景")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	type CallbackConfig struct {
		All    bool   `json:"all"`
		Opaque string `json:"opaque"`
		Target string `json:"target"`
	}
	var conf CallbackConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/query", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request hooks apply failed")
		return
	}

	// Restore the state of transcode.
	backup := conf
	defer func() {
		logger.Tf(ctx, "restore config %v", backup)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/apply", backup, nil)
	}()

	// Enable the callback worker.
	conf.All = true
	conf.Target = fmt.Sprintf("%v/terraform/v1/mgmt/hooks/example?fail=true", *endpoint)
	conf.Opaque = fmt.Sprintf("opaque-%v", rand.Int())
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/apply", &conf, nil); err != nil {
		r0 = errors.Wrapf(err, "request hooks apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Start FFprobe to detect and verify stream.
	duration := time.Duration(*srsFFprobeDuration) * time.Millisecond
	ffprobe := NewFFprobe(func(v *ffprobeClient) {
		v.dvrFile = fmt.Sprintf("srs-ffprobe-%v.flv", streamID)
		v.streamURL = fmt.Sprintf("%v/live/%v.flv", *endpointHTTP, streamID)
		v.duration, v.timeout = duration, time.Duration(*srsFFprobeTimeout)*time.Millisecond
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r2 = ffprobe.Run(ctx, cancel)
	}()

	// Fast quit for probe done.
	select {
	case <-ctx.Done():
	case <-ffprobe.ProbeDoneCtx().Done():
		cancel()
	}

	// Should have no stream for callback failed.
	str, m := ffprobe.Result()
	if len(m.Streams) != 0 {
		r3 = errors.Errorf("invalid streams=%v, %v, %v", len(m.Streams), m.String(), str)
	}
}

func TestScenario_WithStream_CallbackOnRecordMp4(t *testing.T) {
	tc(t, "回呼：透過 on_hls/on_record 回呼觸發 MP4 錄製")
	ctx, cancel := context.WithTimeout(logger.WithContext(context.Background()), time.Duration(*srsLongTimeout)*time.Millisecond)
	defer cancel()

	if *noMediaTest {
		return
	}

	var r0, r1, r2, r3, r4, r5 error
	defer func(ctx context.Context) {
		if err := filterTestError(ctx.Err(), r0, r1, r2, r3, r4, r5); err != nil {
			t.Errorf("Fail for err %+v", err)
		} else {
			logger.Tf(ctx, "test done")
		}
	}(ctx)

	var pubSecret string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/srs/secret/query", nil, &struct {
		Publish *string `json:"publish"`
	}{
		Publish: &pubSecret,
	}); err != nil {
		r0 = err
		return
	}

	type CallbackConfig struct {
		All    bool   `json:"all"`
		Opaque string `json:"opaque"`
		Target string `json:"target"`
	}
	var conf CallbackConfig
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/query", nil, &conf); err != nil {
		r0 = errors.Wrapf(err, "request hooks apply failed")
		return
	}

	// Restore the state of transcode.
	backupCallbackConfig := conf
	defer func() {
		logger.Tf(ctx, "restore config %v", backupCallbackConfig)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/apply", backupCallbackConfig, nil)
	}()

	// Enable the callback worker.
	conf.All = true
	conf.Target = fmt.Sprintf("%v/terraform/v1/mgmt/hooks/example?fail=false", *endpoint)
	conf.Opaque = fmt.Sprintf("opaque-%v", rand.Int())
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/apply", &conf, nil); err != nil {
		r0 = errors.Wrapf(err, "request hooks apply failed")
		return
	}

	// Query the old config.
	backupRecordConfig := make(map[string]interface{})
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/query", nil, &backupRecordConfig); err != nil {
		r0 = errors.Wrapf(err, "request record query failed")
		return
	}
	defer func() {
		logger.Tf(ctx, "restore config %v", backupRecordConfig)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", backupRecordConfig, nil)
	}()

	// Enable the record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{true}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Start FFmpeg to publish stream.
	streamID := fmt.Sprintf("stream-%v-%v", os.Getpid(), rand.Int())
	streamURL := fmt.Sprintf("%v/live/%v?secret=%v", *endpointRTMP, streamID, pubSecret)
	ffmpeg := NewFFmpeg(func(v *ffmpegClient) {
		v.args = []string{
			"-re", "-stream_loop", "-1", "-i", *srsInputFile, "-c", "copy",
			"-f", "flv", streamURL,
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		r1 = ffmpeg.Run(ctx, cancel)
	}()

	// Wait for record to save file.
	select {
	case <-ctx.Done():
	case <-time.After(25 * time.Second):
	}

	// Get the last hook request, should be record begin event.
	var hooksReq string
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/query", nil, &struct {
		Req *string `json:"req"`
	}{
		Req: &hooksReq,
	}); err != nil {
		r0 = errors.Wrapf(err, "request hooks query failed")
		return
	}

	type RecordHooksBeginReq struct {
		RequestID string `json:"request_id"`
		Action    string `json:"action"`
		Opaque    string `json:"opaque"`
		Stream    string `json:"stream"`
		UUID      string `json:"uuid"`
	}
	var beginReq RecordHooksBeginReq
	if err := json.Unmarshal([]byte(hooksReq), &beginReq); err != nil {
		r0 = errors.Wrapf(err, "decode hooks req %v failed", hooksReq)
		return
	}
	if beginReq.Action != "on_record_begin" || beginReq.Stream != streamID || beginReq.UUID == "" {
		r0 = errors.Errorf("invalid hooks req %v", hooksReq)
		return
	}

	// Stop record worker.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/apply", &struct {
		All bool `json:"all"`
	}{false}, nil); err != nil {
		r0 = errors.Wrapf(err, "request record apply failed")
		return
	}
	logger.Tf(ctx, "stop record worker done")

	// Query the record file.
	type RecordFile struct {
		Stream   string  `json:"stream"`
		UUID     string  `json:"uuid"`
		Duration float64 `json:"duration"`
		Progress bool    `json:"progress"`
	}
	var recordFile *RecordFile
	defer func() {
		if recordFile == nil || recordFile.UUID == "" {
			return
		}
		logger.Tf(ctx, "remove record file %v", recordFile)

		// The ctx has already been cancelled by test case, which will cause the request failed.
		ctx := context.Background()
		NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/remove", &struct {
			UUID string `json:"uuid"`
		}{recordFile.UUID}, nil)
	}()
	defer cancel()

	for i := 0; i < 60; i++ {
		files := []RecordFile{}
		if err := NewApi().WithAuth(ctx, "/terraform/v1/hooks/record/files", nil, &files); err != nil {
			r0 = errors.Wrapf(err, "request record files failed")
			return
		}

		for _, file := range files {
			if file.Stream == streamID {
				recordFile = &file
				break
			}
		}

		if recordFile == nil || recordFile.Progress {
			select {
			case <-ctx.Done():
				r0 = errors.Wrapf(ctx.Err(), "record file not found")
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		break
	}

	if recordFile == nil {
		r0 = errors.Errorf("record file not found")
		return
	}
	if recordFile.Progress {
		r0 = errors.Errorf("record file is progress, %v", recordFile)
		return
	}
	if recordFile.Duration < 10 {
		r0 = errors.Errorf("record file duration too short, %v", recordFile)
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Second):
	}
	logger.Tf(ctx, "record ok, file is %v", recordFile)

	// Get the last hook request, should be record end event.
	if err := NewApi().WithAuth(ctx, "/terraform/v1/mgmt/hooks/query", nil, &struct {
		Req *string `json:"req"`
	}{
		Req: &hooksReq,
	}); err != nil {
		r0 = errors.Wrapf(err, "request hooks query failed")
		return
	}

	type RecordHooksEndReq struct {
		RequestID    string `json:"request_id"`
		Action       string `json:"action"`
		Opaque       string `json:"opaque"`
		Stream       string `json:"stream"`
		UUID         string `json:"uuid"`
		ArtifactCode int    `json:"artifact_code"`
		ArtifactPath string `json:"artifact_path"`
		ArtifactURL  string `json:"artifact_url"`
	}
	var endReq RecordHooksEndReq
	if err := json.Unmarshal([]byte(hooksReq), &endReq); err != nil {
		r0 = errors.Wrapf(err, "decode hooks req %v failed", hooksReq)
		return
	}
	if endReq.Action != "on_record_end" || endReq.Stream != streamID || endReq.UUID != recordFile.UUID ||
		endReq.ArtifactCode != 0 || !strings.Contains(endReq.ArtifactPath, endReq.UUID) || endReq.UUID != beginReq.UUID ||
		!strings.Contains(endReq.ArtifactURL, endReq.UUID) {
		r0 = errors.Errorf("invalid hooks req %v", hooksReq)
		return
	}

	logger.Tf(ctx, "record hooks ok, file is %v, hooks is %v", recordFile, hooksReq)
	cancel()
}
