import { describe, expect, test } from "vitest";

import { formatLogs, getErrorWarningCount } from "./utility";

const logs = [
  "Jun 14 16:02:04 proto-miner-001D mcdd[716]: 2024-06-14 16:02:04.512413 | INFO  | mcdd::hashboard::b1::stats_b1:712 | [B1 0] Energy - Power: 747W, Efficiency: infJ/TH",
  "Jun 14 16:02:04 proto-miner-001D mcdd[716]: 2024-06-14 16:02:04.512536 | WARN  | mcdd::pool_interface::pool_manager:379 | [PoolManager] Share rejected: job_id=27, work_id=36, nonce=ca0e07e8, error=STooLowDiff",
  "Jun 14 16:02:06 proto-miner-001D mcdd[716]: 2024-06-14 16:02:06.575555 | ERROR | mcdd::hashboard::hashboard_common:649 | [B1 0] Error during SetWork: NotReady",
  "Jun 14 16:02:06 proto-miner-001D mcdd[716]: 2024-06-14 16:02:06.615707 | DEBUG | mcdd::pool_interface::pool_manager:360 | [PoolManager] Share accepted: job_id=0, work_id=72, nonce=cb6c444c",
  "Jun 14 16:02:06 proto-miner-001D mcdd[716]: 2024-06-14 16:02:06.575555 | ERROR | mcdd::hashboard::hashboard_common:649 | [B1 0] Error during SetWork: NotReady",
];

describe("getFormattedLog", () => {
  let formattedLogs = formatLogs(logs);

  test("should format info log", () => {
    expect(formattedLogs[0].timestamp).toEqual("2024-06-14 16:02:04");
    expect(formattedLogs[0].message).toEqual(
      "mcdd::hashboard::b1::stats_b1:712 | [B1 0] Energy - Power: 747W, Efficiency: infJ/TH",
    );
  });

  test("should format warn log", () => {
    expect(formattedLogs[1].timestamp).toEqual("2024-06-14 16:02:04");
    expect(formattedLogs[1].message).toEqual(
      "mcdd::pool_interface::pool_manager:379 | [PoolManager] Share rejected: job_id=27, work_id=36, nonce=ca0e07e8, error=STooLowDiff",
    );
  });

  test("should format error log", () => {
    expect(formattedLogs[2].timestamp).toEqual("2024-06-14 16:02:06");
    expect(formattedLogs[2].message).toEqual(
      "mcdd::hashboard::hashboard_common:649 | [B1 0] Error during SetWork: NotReady",
    );
  });

  test("should format debug log", () => {
    expect(formattedLogs[3].timestamp).toEqual("2024-06-14 16:02:06");
    expect(formattedLogs[3].message).toEqual(
      "mcdd::pool_interface::pool_manager:360 | [PoolManager] Share accepted: job_id=0, work_id=72, nonce=cb6c444c",
    );
  });
});

describe("getErrorWarningCount", () => {
  test("should return error and warning count", () => {
    const { error, warning } = getErrorWarningCount(logs);

    expect(error).toEqual(2);
    expect(warning).toEqual(1);
  });
});
