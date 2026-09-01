package main

import (
	"testing"
)

func TestUtils_RebuildStreamURL(t *testing.T) {
	urlSamples := []struct {
		url     string
		rebuild string
	}{
		{url: "rtsp://121.1.2.3", rebuild: "rtsp://121.1.2.3"},
		{url: "rtsp://121.1.2.3/Streaming/Channels/101", rebuild: "rtsp://121.1.2.3/Streaming/Channels/101"},
		{url: "rtsp://121.1.2.3:554/Streaming/Channels/101", rebuild: "rtsp://121.1.2.3:554/Streaming/Channels/101"},
		{url: "rtsp://121.1.2.3:554/Streaming/Channels/101?k=v", rebuild: "rtsp://121.1.2.3:554/Streaming/Channels/101?k=v"},
		{url: "rtsp://CamViewer:abc123@121.1.2.3:554/Streaming/Channels/101", rebuild: "rtsp://CamViewer:abc123@121.1.2.3:554/Streaming/Channels/101"},
		{url: "rtsp://CamViewer:abc123?!@121.1.2.3:554/Streaming/Channels/101", rebuild: "rtsp://CamViewer:abc123%3F%21@121.1.2.3:554/Streaming/Channels/101"},
		{url: "rtsp://CamViewer:abc123@?!@121.1.2.3:554/Streaming/Channels/101", rebuild: "rtsp://CamViewer:abc123%40%3F%21@121.1.2.3:554/Streaming/Channels/101"},
		{url: "rtsp://CamViewer:abc123@?!@121.1.2.3:554/Streaming/Channels/101?k=v", rebuild: "rtsp://CamViewer:abc123%40%3F%21@121.1.2.3:554/Streaming/Channels/101?k=v"},
		{url: "rtsp://CamViewer:abc123@?!@121.1.2.3:554", rebuild: "rtsp://CamViewer:abc123%40%3F%21@121.1.2.3:554"},
		{url: "rtsp://Cam@Viewer:abc123@?!@121.1.2.3:554", rebuild: "rtsp://Cam%40Viewer:abc123%40%3F%21@121.1.2.3:554"},
		{url: "rtsp://CamViewer:abc123@?!~#$%^&*()_+-=\\|?@121.1.2.3:554/Streaming/Channels/101", rebuild: "rtsp://CamViewer:abc123%40%3F%21~%23$%25%5E&%2A%28%29_+-=%5C%7C%3F@121.1.2.3:554/Streaming/Channels/101"},
		{url: "rtsp://CamViewer:abc123@347?1!@121.1.2.3:554/Streaming/Channels/101", rebuild: "rtsp://CamViewer:abc123%40347%3F1%21@121.1.2.3:554/Streaming/Channels/101"},
		{url: "srt://213.171.194.158:10080", rebuild: "srt://213.171.194.158:10080"},
		{url: "srt://213.171.194.158:10080?streamid=#!::r=live/primary,latency=20,m=request", rebuild: "srt://213.171.194.158:10080?streamid=#!::r=live/primary,latency=20,m=request"},
	}
	for _, urlSample := range urlSamples {
		if r0, err := RebuildStreamURL(urlSample.url); err != nil {
			t.Errorf("Fail for err %+v", err)
			return
		} else if rebuild := r0.String(); rebuild != urlSample.rebuild {
			t.Errorf("rebuild url %v failed, expect %v, actual %v",
				urlSample.url, urlSample.rebuild, rebuild)
			return
		}
	}
}

func TestUtils_CheckRTMPOutputURL(t *testing.T) {
	okSamples := []string{
		"rtmp://localhost/live/livestream",
		"rtmp://localhost/live/livestream?secret=xxx",
		"rtmps://dummy1234.global-contribute.live-video.net/app/sk_1234567890abcdef",
		"rtmp://host/app/foo/bar",
		"rtmp://host/app/live/key?secret=abc",
		// srt has no app/stream path, always skipped.
		"srt://213.171.194.158:10080?streamid=#!::r=live/primary,latency=20,m=request",
	}
	for _, outputURL := range okSamples {
		if err := CheckRTMPOutputURL(outputURL); err != nil {
			t.Errorf("CheckRTMPOutputURL(%v) should pass, got %+v", outputURL, err)
			return
		}
	}

	invalidSamples := []string{
		// The single path segment becomes the app name, playpath is empty.
		"rtmps://dummy1234.global-contribute.live-video.net/sk_1234567890abcdef",
		"rtmp://host/live",
		// No path at all.
		"rtmps://dummy1234.global-contribute.live-video.net",
		"rtmp://host/",
		// Trailing slash, empty playpath.
		"rtmp://host/app/",
	}
	for _, outputURL := range invalidSamples {
		if err := CheckRTMPOutputURL(outputURL); err == nil {
			t.Errorf("CheckRTMPOutputURL(%v) should fail", outputURL)
			return
		}
	}
}

func TestUtils_NormalizeRTMPConfig(t *testing.T) {
	for _, e := range []struct {
		server, secret             string
		expectServer, expectSecret string
	}{
		// Full URL pasted into server, split at the first path segment.
		{server: "rtmps://dummy1234.global-contribute.live-video.net/app/sk_1234567890abcdef",
			expectServer: "rtmps://dummy1234.global-contribute.live-video.net/app", expectSecret: "sk_1234567890abcdef"},
		// Query on the playpath stays with the secret.
		{server: "rtmps://host/app/key?token=abc", expectServer: "rtmps://host/app", expectSecret: "key?token=abc"},
		// Deeper playpath: first segment is the app, rest is the secret.
		{server: "rtmp://host/app/foo/bar", expectServer: "rtmp://host/app", expectSecret: "foo/bar"},
		// Server+secret already split: unchanged.
		{server: "rtmp://host/app", secret: "key", expectServer: "rtmp://host/app", expectSecret: "key"},
		// Single segment (no app): unchanged.
		{server: "rtmps://host/sk_xxx", expectServer: "rtmps://host/sk_xxx", expectSecret: ""},
		// Non-rtmp scheme: unchanged.
		{server: "srt://host:10080?streamid=#!::r=live,latency=20", expectServer: "srt://host:10080?streamid=#!::r=live,latency=20", expectSecret: ""},
	} {
		if s, k := NormalizeRTMPConfig(e.server, e.secret); s != e.expectServer || k != e.expectSecret {
			t.Errorf("NormalizeRTMPConfig(%v, %v) = (%v, %v), expect (%v, %v)",
				e.server, e.secret, s, k, e.expectServer, e.expectSecret)
			return
		}
	}
}

func TestUtils_ParseFFmpegLogs(t *testing.T) {
	for _, e := range []struct {
		log   string
		ts    string
		speed string
	}{
		{log: "time=00:10:09.138 speed=1x", ts: "00:10:09.138", speed: "1x"},
		{log: "size=18859kB time=00:10:09.138 speed=1x", ts: "00:10:09.138", speed: "1x"},
		{log: "size=18859kB time=00:10:09.138 speed=1x dup=1", ts: "00:10:09.138", speed: "1x"},
		{log: "size=18859kB time=00:10:09.138 bitrate=253.5kbits/s speed=1x dup=1", ts: "00:10:09.138", speed: "1x"},
		{log: "size=18859kB time=00:10:09.38 bitrate=253.5kbits/s speed=1x", ts: "00:10:09.38", speed: "1x"},
		{log: "frame=184 fps=9.7 q=28.0 size=364kB time=00:00:19.41 bitrate=153.7kbits/s dup=0 drop=235 speed=1.03x", ts: "00:00:19.41", speed: "1.03x"},
	} {
		if ts, speed, err := ParseFFmpegCycleLog(e.log); err != nil {
			t.Errorf("Fail parse %v for err %+v", e, err)
		} else if ts != e.ts {
			t.Errorf("Fail for ts %v of %v", ts, e)
		} else if speed != e.speed {
			t.Errorf("Fail for speed %v of %v", speed, e)
		}
	}
}

func TestUtils_ComputeStreamFPS(t *testing.T) {
	t.Run("stable cfr", func(t *testing.T) {
		fps, err := computeStreamFPS([]float64{0.0333, 0.0334, 0.0333, 0.0334})
		if err != nil {
			t.Fatalf("compute fps err %+v", err)
		}
		if fps.Abnormal {
			t.Fatalf("stable fps should not be abnormal, got %+v", fps)
		}
		if fps.FPS != 30.0 {
			t.Fatalf("invalid fps %+v", fps)
		}
	})

	t.Run("small timestamp noise", func(t *testing.T) {
		fps, err := computeStreamFPS([]float64{0.031, 0.036, 0.032, 0.035, 0.033})
		if err != nil {
			t.Fatalf("compute fps err %+v", err)
		}
		if fps.Abnormal {
			t.Fatalf("small timestamp noise should not be abnormal, got %+v", fps)
		}
	})

	t.Run("large jitter", func(t *testing.T) {
		fps, err := computeStreamFPS([]float64{0.016, 0.050, 0.017, 0.049, 0.018})
		if err != nil {
			t.Fatalf("compute fps err %+v", err)
		}
		if !fps.Abnormal {
			t.Fatalf("large jitter should be abnormal, got %+v", fps)
		}
	})
}
