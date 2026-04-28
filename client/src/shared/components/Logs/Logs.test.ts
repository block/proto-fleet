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

describe("syslog-only (BX firmware) logs", () => {
  const bxLogs = [
    "Feb 23 12:33:24 bx-miner mcdd[664]: | INFO  | some::module:42 | Mining started",
    "Feb  5 08:02:55 bx-miner mcdd[664]: | WARN  | some::module:55 | High temp detected",
    "Feb  5 08:02:55 bx-miner mcdd[664]: | ERROR | some::module:88 | Fan failure",
  ];

  let formattedBxLogs = formatLogs(bxLogs);

  test("should extract syslog timestamp when no mcdd timestamp is present", () => {
    expect(formattedBxLogs[0].timestamp).toEqual("Feb 23 12:33:24");
    expect(formattedBxLogs[0].message).toEqual("some::module:42 | Mining started");
  });

  test("should handle single-digit day padding in syslog timestamp", () => {
    // Syslog uses double-space padding for single-digit days: "Feb  5 08:02:55"
    expect(formattedBxLogs[1].timestamp).toEqual("Feb 5 08:02:55");
    expect(formattedBxLogs[1].message).toEqual("some::module:55 | High temp detected");
  });

  test("should format error log with syslog-only timestamp", () => {
    expect(formattedBxLogs[2].timestamp).toEqual("Feb 5 08:02:55");
    expect(formattedBxLogs[2].message).toEqual("some::module:88 | Fan failure");
  });

  test("should parse syslog-style (BX) timestamp without mcdd prefix", () => {
    const bxDirectLogs = ["Feb 26 05:12:34 miner-host | INFO  | some::module:123 | BX log message here"];
    const formatted = formatLogs(bxDirectLogs);
    expect(formatted[0].timestamp).toEqual("Feb 26 05:12:34");
    expect(formatted[0].message).toEqual("some::module:123 | BX log message here");
  });
});

describe("getErrorWarningCount", () => {
  test("should return error and warning count", () => {
    const { error, warning } = getErrorWarningCount(logs);

    expect(error).toEqual(2);
    expect(warning).toEqual(1);
  });
});
