//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {render, waitFor} from "@testing-library/react";
import {describe, expect, it, vi, beforeEach} from "vitest";
import SrsConsole from "./SrsConsole";
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
});
