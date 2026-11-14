// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// rtcServerConf is the SRS configuration block Oryx ships. It is parsed (not duplicated in Go) so
// the diagnostics always describe the configuration actually handed to SRS.
const rtcServerConf = "containers/conf/srs.release.conf"

// rtcDiagClient is one live WebRTC peer as SRS reports it.
type rtcDiagClient struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	IP       string  `json:"ip"`
	App      string  `json:"app,omitempty"`
	Stream   string  `json:"stream,omitempty"`
	AliveSec float64 `json:"aliveSec"`
	RecvKbps float64 `json:"recvKbps"`
	SendKbps float64 `json:"sendKbps"`
}

// rtcDiagInfo is the WebRTC view: how SRS advertises itself, and who is connected right now.
type rtcDiagInfo struct {
	// Whether WebRTC is enabled for the server and the default vhost.
	Enabled   bool   `json:"enabled"`
	RtmpToRtc bool   `json:"rtmpToRtc"`
	RtcToRtmp bool   `json:"rtcToRtmp"`
	UDPPort   int    `json:"udpPort"`
	Candidate string `json:"candidate"`
	// Local interface addresses, and the host the browser used to reach us.
	LocalIPs    []string `json:"localIps"`
	RequestHost string   `json:"requestHost"`
	// Whether the candidate address is one a remote browser could plausibly reach. Oryx runs SRS
	// in a container, so a Docker-internal address is not reachable from the LAN or the internet,
	// and that failure is silent in the browser: ICE just never connects.
	CandidateReachable bool `json:"candidateReachable"`
	// The candidate SRS actually advertises (from its own refresh logic, else the config/env).
	EffectiveCandidate string           `json:"effectiveCandidate"`
	Clients            []*rtcDiagClient `json:"clients"`
	// Machine-readable advisories for the UI.
	Warnings []string `json:"warnings"`
}

// rtcConfigFile returns the SRS config path next to the platform binary.
func rtcConfigFile() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), rtcServerConf)
}

// parseRtcConfig reads the rtc_server / vhost rtc switches out of the shipped SRS config.
func parseRtcConfig(path string) (enabled bool, udpPort int, candidate string, rtmpToRtc, rtcToRtmp bool, err error) {
	var b []byte
	if b, err = os.ReadFile(path); err != nil {
		return false, 0, "", false, false, errors.Wrapf(err, "read %v", path)
	}
	text := string(b)
	udpPort = 8000

	if m := regexp.MustCompile(`(?m)^rtc_server\s*\{[^}]*`).FindString(text); m != "" {
		enabled = regexp.MustCompile(`enabled\s+on`).MatchString(m)
		if p := regexp.MustCompile(`listen\s+(\d+)`).FindStringSubmatch(m); len(p) == 2 {
			if v, e := strconv.Atoi(p[1]); e == nil {
				udpPort = v
			}
		}
		if c := regexp.MustCompile(`(?m)^\s*candidate\s+([^;\r\n]*);`).FindStringSubmatch(m); len(c) == 2 {
			candidate = strings.TrimSpace(c[1])
		}
	}
	if m := regexp.MustCompile(`(?ms)^\s*rtc\s*\{.*?\n\s*\}`).FindString(text); m != "" {
		rtmpToRtc = regexp.MustCompile(`rtmp_to_rtc\s+on`).MatchString(m)
		rtcToRtmp = regexp.MustCompile(`rtc_to_rtmp\s+on`).MatchString(m)
	}
	return
}

// localIPv4s lists the addresses a remote peer could be told about.
func localIPv4s() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips
}

// queryRtcClients asks SRS for its live WebRTC peers. SRS exposes them through the same clients
// API as RTMP, tagged with rtc-publish / rtc-play.
func queryRtcClients(ctx context.Context) ([]*rtcDiagClient, error) {
	host := os.Getenv("SRS_HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(fmt.Sprintf("http://%v:1985/api/v1/clients/", host))
	if err != nil {
		return nil, errors.Wrapf(err, "list clients")
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "read clients")
	}

	var parsed struct {
		Clients []struct {
			ID    string  `json:"id"`
			Type  string  `json:"type"`
			IP    string  `json:"ip"`
			Name  string  `json:"name"`
			Alive float64 `json:"alive"`
			Kbps  struct {
				Recv30s float64 `json:"recv_30s"`
				Send30s float64 `json:"send_30s"`
			} `json:"kbps"`
			URL string `json:"url"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.Wrapf(err, "parse clients %v", string(body))
	}

	var r0 []*rtcDiagClient
	for _, c := range parsed.Clients {
		if !strings.HasPrefix(c.Type, "rtc-") {
			continue
		}
		app, stream := "", ""
		if parts := strings.Split(strings.TrimPrefix(c.URL, "/"), "/"); len(parts) >= 2 {
			app, stream = parts[0], parts[1]
		}
		r0 = append(r0, &rtcDiagClient{
			ID: c.ID, Type: c.Type, IP: c.IP, App: app, Stream: stream,
			AliveSec: c.Alive, RecvKbps: c.Kbps.Recv30s, SendKbps: c.Kbps.Send30s,
		})
	}
	return r0, nil
}

// handleMgmtRtcQuery reports the WebRTC configuration and live peers. The dominant real-world
// failure is an unreachable ICE candidate: Oryx runs SRS in a container, so SRS advertises the
// Docker bridge address unless a candidate is configured, and browsers outside that network simply
// never connect — with no error anywhere except a stuck player.
func handleMgmtRtcQuery(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/rtc/query"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			info := &rtcDiagInfo{Candidate: os.Getenv("CANDIDATE")}

			path := rtcConfigFile()
			if path != "" {
				enabled, port, candidate, rtmpToRtc, rtcToRtmp, err := parseRtcConfig(path)
				if err != nil {
					// Config unreadable is itself a finding: report it instead of pretending WebRTC
					// is simply off.
					return err
				}
				info.Enabled, info.UDPPort, info.RtmpToRtc, info.RtcToRtmp = enabled, port, rtmpToRtc, rtcToRtmp
				if info.Candidate == "" {
					info.Candidate = candidate
				}
			}
			info.LocalIPs = localIPv4s()

			// The address the browser actually used to reach us. Stripping the port gives the host
			// a remote peer can reach, which is what an ICE candidate must match.
			info.RequestHost = r.Host
			if host, _, err := net.SplitHostPort(r.Host); err == nil {
				info.RequestHost = host
			}

			// The config may hold an $ENV placeholder; resolve it the way SRS does before judging it.
			if strings.HasPrefix(info.Candidate, "$") {
				info.Candidate = os.Getenv(strings.TrimPrefix(info.Candidate, "$"))
			}

			// Decide what SRS will actually advertise, and whether a peer outside the container can
			// reach it. Both facts are required: "which address", then "is it dialable".
			switch {
			case info.Candidate == "" || info.Candidate == "*":
				// SRS auto-detects and picks a local interface. Inside a container that is the
				// bridge address, which the LAN cannot route to.
				if len(info.LocalIPs) > 0 {
					info.EffectiveCandidate = info.LocalIPs[0]
				} else {
					info.EffectiveCandidate = "auto"
				}
				info.CandidateReachable = false
				info.Warnings = append(info.Warnings, "candidate-auto")
			case net.ParseIP(info.Candidate) != nil:
				info.EffectiveCandidate = info.Candidate
				info.CandidateReachable = false
				for _, ip := range info.LocalIPs {
					if ip == info.Candidate {
						info.CandidateReachable = true
						break
					}
				}
				if !info.CandidateReachable {
					info.Warnings = append(info.Warnings, "candidate-not-local")
				} else if info.RequestHost != "" && net.ParseIP(info.RequestHost) != nil && info.Candidate != info.RequestHost {
					// Address is ours, but not the one peers dialed: they will try the wrong one.
					info.CandidateReachable = false
					info.Warnings = append(info.Warnings, "candidate-mismatch")
				}
			default:
				// A hostname candidate cannot be verified without DNS resolution from the peer.
				info.EffectiveCandidate = info.Candidate
				info.CandidateReachable = false
				info.Warnings = append(info.Warnings, "candidate-hostname")
			}

			clients, err := queryRtcClients(ctx)
			if err != nil {
				// Not fatal: SRS may still be starting. Surface it as a warning.
				logger.Wf(ctx, "rtc query clients, err=%v", err)
				info.Warnings = append(info.Warnings, "clients-unavailable")
			}
			info.Clients = clients

			ohttp.WriteData(ctx, w, r, info)
			logger.Tf(ctx, "rtc query ok, enabled=%v udp=%v candidate=%v clients=%v warnings=%v",
				info.Enabled, info.UDPPort, info.EffectiveCandidate, len(clients), info.Warnings)
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
