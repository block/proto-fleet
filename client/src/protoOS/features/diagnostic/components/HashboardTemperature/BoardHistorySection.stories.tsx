import { useMemo } from "react";

import BoardHistorySection from "./BoardHistorySection";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";

type Scenario = "history" | "quiet" | "unavailable" | "evidenceUnavailable";

const MAC = "02:00:00:00:00:0a";
const HOUR_MS = 60 * 60 * 1000;
const NOW = Date.now();

// Wire JSON as the controller sends it; the generated types are private to the adapter.
const events = (startMs: number, endMs: number) => {
  const at = (hoursAgo: number) => new Date(NOW - hoursAgo * HOUR_MS).toISOString();
  const episode = (hoursAgo: number, cause: string) => [
    {
      type: "HASHBOARD_EVENT_TYPE_MINING_STATE_CHANGE",
      time: at(hoursAgo),
      fromState: "MINING_STATE_MINING",
      toState: "MINING_STATE_ERROR",
    },
    { type: "HASHBOARD_EVENT_TYPE_ERROR", time: at(hoursAgo), lastSeen: at(hoursAgo - 0.3), count: 36, cause },
    { type: "HASHBOARD_EVENT_TYPE_STACK_DROPOUT", time: at(hoursAgo - 0.1), stack: 2, detail: "stack 2 stopped" },
    {
      type: "HASHBOARD_EVENT_TYPE_MINING_STATE_CHANGE",
      time: at(hoursAgo - 0.3),
      fromState: "MINING_STATE_ERROR",
      toState: "MINING_STATE_MINING",
    },
  ];
  return {
    macAddress: MAC,
    effectiveWindow: { start: new Date(startMs).toISOString(), end: new Date(endMs).toISOString() },
    events: [
      ...episode(20, "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE"),
      {
        type: "HASHBOARD_EVENT_TYPE_LAST_SHUTDOWN",
        time: at(11),
        lastSeen: at(10.5),
        count: 60,
        cause: "HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE",
      },
      ...episode(6, "HASHBOARD_ERROR_CAUSE_ASIC_OVER_TEMPERATURE"),
      { type: "HASHBOARD_EVENT_TYPE_ISOLATION", time: at(0), isolatedBy: "operator" },
    ].filter((event) => Date.parse(event.time) >= startMs && Date.parse(event.time) < endMs),
  };
};

const daysAgo = (days: number) => new Date(NOW - days * 24 * HOUR_MS).toISOString();
const summaries = [
  {
    evidenceId: "3f6c2a10-8b1e-4c7d-9a55-0e2d4b7c9f11",
    episodeId: "episode-42",
    macAddress: MAC,
    trigger: "power_recovery",
    causes: ["HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE"],
    capturedAt: daysAgo(1),
    capturedBy: "ct1-a",
    truncated: true,
    payloadBytes: 2048,
  },
  {
    evidenceId: "9d0b7e54-21aa-4f3c-8e6d-5b1c0a9e3d27",
    episodeId: "episode-39",
    macAddress: MAC,
    trigger: "ready_recovery",
    causes: ["HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE"],
    capturedAt: daysAgo(6),
    capturedBy: "ct1-b",
    truncated: false,
    payloadBytes: 1800,
  },
];
const document = (summary: (typeof summaries)[number]) => ({
  version: 1,
  mac_address: MAC,
  captured_at: summary.capturedAt,
  trigger: summary.trigger,
  events: {
    stack_dropouts: [
      {
        stack: 2,
        detected_at: summary.capturedAt,
        cleared_at: summary.truncated ? undefined : summary.capturedAt,
      },
    ],
  },
  logs: ["hashboard log lines stay in the download"],
  sources: {
    health: { status: "ok", count: 1 },
    asic_frames: summary.truncated ? { status: "error", detail: "frame read timed out" } : { status: "ok", count: 1 },
    can_status: { status: "unavailable" },
    logs: { status: "ok", count: 12 },
  },
  omitted: summary.truncated ? ["asic_frames"] : [],
});

const offline = () => Promise.reject(Object.assign(new Error("The controller did not respond."), { status: 503 }));

const BoardHistoryPreview = ({ scenario }: { scenario: Scenario }) => {
  const adapter = useMemo(() => {
    const client: Partial<MdkV2Client> = {
      telemetryServiceFollowAsicHealth: async function* () {},
      hashboardServiceGetHashboardEvents: ((query) => {
        if (scenario === "unavailable") return offline();
        const endMs = query["window.end"] ? Date.parse(query["window.end"]) : NOW;
        const startMs = Date.parse(query["window.start"] ?? new Date(endMs - 24 * HOUR_MS).toISOString());
        const data = scenario === "quiet" ? { ...events(startMs, endMs), events: [] } : events(startMs, endMs);
        return Promise.resolve({ data });
      }) as MdkV2Client["hashboardServiceGetHashboardEvents"],
      hashboardServiceListRecoveryEvidence: (() =>
        scenario === "evidenceUnavailable"
          ? offline()
          : Promise.resolve({
              data: { evidence: scenario === "quiet" ? [] : summaries },
            })) as MdkV2Client["hashboardServiceListRecoveryEvidence"],
      hashboardServiceGetRecoveryEvidence: (({ evidenceId }) => {
        const summary = summaries.find((entry) => entry.evidenceId === evidenceId) ?? summaries[0];
        return Promise.resolve({ data: { summary, evidenceJson: JSON.stringify(document(summary)) } });
      }) as MdkV2Client["hashboardServiceGetRecoveryEvidence"],
    };
    return new MdkV2Adapter(client as MdkV2Client);
  }, [scenario]);

  return (
    <MinerHostingContext.Provider
      value={{
        adapter,
        resolution: { status: "ready", adapterId: "mdk-v2", adapter },
        minerRoot: "",
        closeButton: null,
        mode: "direct",
        metadata: {},
      }}
    >
      <div className="min-h-screen bg-surface-base p-6 text-text-primary laptop:p-10">
        <div className="mx-auto max-w-[1100px]">
          <BoardHistorySection mac={MAC} />
        </div>
      </div>
    </MinerHostingContext.Provider>
  );
};

export const RecentFaults = () => <BoardHistoryPreview scenario="history" />;
export const Quiet = () => <BoardHistoryPreview scenario="quiet" />;
export const HistoryUnavailable = () => <BoardHistoryPreview scenario="unavailable" />;
export const EvidenceUnavailable = () => <BoardHistoryPreview scenario="evidenceUnavailable" />;

export default { title: "Proto OS/Diagnostic/Board History", component: BoardHistorySection };
