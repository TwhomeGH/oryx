//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {render, screen, waitFor} from "@testing-library/react";
import {describe, expect, it, vi, beforeEach} from "vitest";
import SrsConsole, {SrsStreams, fmtBitrate, fmtBytes, fmtClock, fmtFixed, fmtFPSInterval, fmtFPSIntervalRange, fmtPercent, fmtSec, mergeFPSProbe} from "./SrsConsole";
import axios from "axios";

vi.mock("axios");
vi.mock("react-i18next", () => ({
  useTranslation: () => ({t: (k) => k}),
}));

beforeEach(() => {
  vi.resetAllMocks();
});

describe("SrsConsole", () => {
  it("renders the console tabs", async () => {
    axios.get.mockResolvedValue({data: {code: 0, data: {
      system: {uptime: 100, cpu_percent: 0.05, mem_ram_percent: 0.3, mem_ram_kbyte: 100000,
        cpus: 8, cpus_online: 8, load_1m: 1, load_5m: 2, load_15m: 3,
        net_sample_time: 0, srs_sample_time: 0, net_recv_bytes: 0, net_send_bytes: 0,
        conn_srs: 10, conn_sys: 20, conn_sys_udp: 5},
      self: {version: "7.0.0", srs_uptime: 1000, cpu_percent: 0.1, mem_percent: 0.2,
        mem_kbyte: 50000, pid: 123, ppid: 1},
      ok: true,
    }}});

    const {getByText} = render(<SrsConsole />);

    // Tabs render (translation keys since i18n is mocked).
    expect(getByText("console.overview")).toBeTruthy();
    expect(getByText("console.vhosts")).toBeTruthy();
    expect(getByText("console.streams")).toBeTruthy();
    expect(getByText("console.clients")).toBeTruthy();
    expect(getByText("console.configs")).toBeTruthy();

    // Overview data renders after load. Card header is "SRS 7.0.0".
    await waitFor(() => {
      expect(getByText("SRS 7.0.0")).toBeTruthy();
    });
  });

  it("formats missing and malformed numeric API fields safely", () => {
    expect(fmtBytes(undefined)).toBe("-");
    expect(fmtBytes("")).toBe("-");
    expect(fmtBytes("2048")).toBe("2.0 KB");

    expect(fmtBitrate(undefined)).toBe("-");
    expect(fmtBitrate("")).toBe("-");
    expect(fmtBitrate("1200")).toBe("1.2 Kbps");

    expect(fmtPercent(undefined)).toBe("-");
    expect(fmtPercent("")).toBe("-");
    expect(fmtPercent("0.12")).toBe("12.00%");

    expect(fmtSec(undefined)).toBe("-");
    expect(fmtSec("")).toBe("-");
    expect(fmtSec("59")).toBe("59s");

    expect(fmtFixed(undefined)).toBe("-");
    expect(fmtFixed("")).toBe("-");
    expect(fmtFixed("59.940")).toBe("59.9");

    expect(fmtFPSIntervalRange(undefined)).toBe("-");
    expect(fmtFPSIntervalRange(60)).toBe("13.9-20.8");
    expect(fmtFPSIntervalRange(25)).toBe("33.3-50.0");
    expect(fmtFPSInterval(59.6)).toBe("16.8");
    expect(fmtClock(undefined)).toBe("-");
    expect(fmtClock(12 * 3600 * 1000 + 34 * 60 * 1000 + 56 * 1000)).toMatch(/^\d{2}:34:56$/);
  });

  it("shows measured fps even when frame interval variation is marked", async () => {
    axios.get.mockImplementation((url) => {
      if (url === "/api/v1/vhosts/") {
        return Promise.resolve({data: {code: 0, vhosts: [{id: "v1", name: "__defaultVhost__"}]}});
      }
      if (url === "/api/v1/streams/") {
        return Promise.resolve({data: {code: 0, streams: [{
          id: "s1",
          name: "livestream",
          app: "live",
          vhost: "v1",
          publish: {active: true, cid: "c1"},
          kbps: {recv_30s: 1200, send_30s: 2400},
          clients: 1,
        }]}});
      }
      return Promise.reject(new Error(`unexpected url ${url}`));
    });

    render(<SrsStreams handleError={vi.fn()} initialFps={{
      s1: {fps: 59.94, baselineFps: 60.0, jitterMs: 7.2, variable: true},
    }} />);

    await waitFor(() => expect(screen.getByText("59.9 fps")).toBeTruthy());
    expect(screen.getByText("console.fpsAbnormal")).toBeTruthy();
  });

  it("keeps a rolling fps baseline from the stream's own samples", () => {
    expect(mergeFPSProbe(undefined, {fps: 60.0}, 1000).baselineFps).toBe(60.0);
    expect(mergeFPSProbe({baselineFps: 60.0}, {fps: 58.0}, 2000).baselineFps).toBeCloseTo(59.6);
    expect(mergeFPSProbe({baselineFps: 60.0}, {fps: 58.0}, 3000).variable).toBe(false);
    expect(mergeFPSProbe({baselineFps: 60.0}, {fps: 40.0}, 4000).variable).toBe(true);
    expect(mergeFPSProbe({baselineFps: 60.0}, {jitter_ms: 7.2}, 5000).baselineFps).toBe(60.0);
    expect(mergeFPSProbe({baselineFps: 60.0}, {jitter_ms: 7.2}, 6000).jitterMs).toBe(7.2);
    expect(mergeFPSProbe({baselineFps: 60.0}, {fps: 59.6, jitter_ms: 1.5}, 7000).updatedAt).toBe(7000);
  });
});
