import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { MonitorSnapshot } from "@nextshell/core";
import { SystemInfoPanel } from "./SystemInfoPanel";

const snapshot: MonitorSnapshot = {
  connectionId: "conn-1",
  loadAverage: [0.12, 0.34, 0.56],
  cpuPercent: 12,
  memoryPercent: 5,
  memoryUsedMb: 716,
  memoryTotalMb: 15462.4,
  swapPercent: 0,
  swapUsedMb: 0,
  swapTotalMb: 1945.6,
  diskPercent: 2,
  diskUsedGb: 8.1,
  diskTotalGb: 492,
  networkInMbps: 1.2,
  networkOutMbps: 0.3,
  networkInterface: "eth0",
  networkInterfaceOptions: ["eth0"],
  processes: [],
  capturedAt: "2026-03-24T00:00:00.000Z",
};

describe("SystemInfoPanel", () => {
  test("renders detailed usage values inside the metric track", () => {
    const html = renderToStaticMarkup(
      <SystemInfoPanel
        monitorSessionEnabled
        hasVisibleTerminal
        snapshot={snapshot}
      />,
    );

    expect(html).toMatch("monitor-inline-detail");
    expect(html).toMatch("716M");
    expect(html).toMatch("15.1G");
    expect(html).toMatch("0M");
    expect(html).toMatch("1.9G");
    expect(html).toMatch("8.1G");
    expect(html).toMatch("492.0G");
    expect(html).toMatch("上行");
    expect(html).toMatch("下行");
  });

  test("explains why monitor manager actions are disabled", () => {
    const html = renderToStaticMarkup(
      <SystemInfoPanel
        monitorSessionEnabled
        hasVisibleTerminal={false}
        snapshot={snapshot}
        monitorActionsDisabled
        onOpenProcessManager={() => {}}
        onOpenNetworkMonitor={() => {}}
      />,
    );

    expect(html).toMatch("需先连接 SSH 终端后可用");
  });
});
