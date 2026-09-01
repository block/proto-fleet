/**
 * Fleet-native adapter (Strategy 1).
 *
 * Identity + KPIs are sourced *entirely from the fleet server* via the existing
 * FleetManagementService.ListMinerStateSnapshots RPC (over /api-proxy) — no
 * direct call to the miner. That's the strategy's whole thesis: one backend,
 * any miner, regardless of on-device OS/firmware.
 *
 * The ASIC mini-grid is the honest catch. The fleet collector already builds a
 * full DeviceMetrics (HashBoards → ASICs) in memory
 * (server/.../plugins/mappers/sdk_mapper.go) but the persistence layer DROPS the
 * component arrays (server/.../timescaledb/telemetry_store.go:366, device-level
 * scalars only), and NO client-facing RPC exposes them. So here the grid is
 * SYNTHESIZED from the device-level temperature — a faithful picture of what a
 * fleet-native view can show *today*. Making it real needs a new `prototype/v1`
 * Connect RPC that calls miner.GetDeviceMetrics(ctx) on demand (bypassing the
 * lossy DB); the mechanism exists (interfaces/miner.go:59), it's just unwired.
 */
import type { SingleMinerAdapter } from "../shared/adapter";
import { type FlowTracer, NO_TRACE } from "../shared/flowTrace";
import type { AsicCell, AsicHealth, HashboardSummary, MinerStatus, SingleMinerSnapshot } from "../shared/types";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  DeviceStatus,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const SYNTH_BOARDS = 3;
const SYNTH_ASICS_PER_BOARD = 66;

function current(arr: { value: number }[]): number | null {
  return arr.length ? arr[0].value : null;
}

function mapStatus(snapshot: MinerStateSnapshot): MinerStatus {
  switch (snapshot.deviceStatus) {
    case DeviceStatus.ERROR:
      return "error";
    case DeviceStatus.OFFLINE:
    case DeviceStatus.INACTIVE:
      return "offline";
    case DeviceStatus.MAINTENANCE:
    case DeviceStatus.UPDATING:
      return "paused";
    case DeviceStatus.ONLINE:
      return (current(snapshot.hashrate) ?? 0) > 0 ? "mining" : "paused";
    default:
      return "offline";
  }
}

/** Synthesize a per-chip grid around the device-level temperature. */
function synthGrid(deviceTempC: number | null, deviceHashrateThs: number | null): HashboardSummary[] {
  const baseTemp = deviceTempC ?? 65;
  const perBoardThs = (deviceHashrateThs ?? 0) / SYNTH_BOARDS;
  return Array.from({ length: SYNTH_BOARDS }, (_, board) => {
    const boardBase = baseTemp + board * 2;
    const asics: AsicCell[] = Array.from({ length: SYNTH_ASICS_PER_BOARD }, (_, index) => {
      const wobble = ((index * 7 + board * 13) % 11) - 5;
      const tempC = boardBase + wobble;
      let health: AsicHealth = "ok";
      if (tempC >= boardBase + 4) health = "warn";
      if (tempC >= boardBase + 8) health = "error";
      if ((index * 3 + board) % 41 === 0) health = "off";
      return {
        index,
        tempC: health === "off" ? null : Math.round(tempC * 10) / 10,
        hashrateThs: health === "off" ? 0 : Math.round((perBoardThs / SYNTH_ASICS_PER_BOARD) * 1000) / 1000,
        health,
      };
    });
    const live = asics.filter((a) => a.health !== "off");
    return {
      serialNumber: `HB-${board}`,
      index: board,
      tempC: live.length ? Math.max(...live.map((a) => a.tempC ?? 0)) : null,
      hashrateThs: Math.round(perBoardThs * 100) / 100,
      asics,
    };
  });
}

export class FleetAdapter implements SingleMinerAdapter {
  readonly source = "Fleet server (Connect RPC)";

  /** `target` is an IP (matched as /32) — the single-miner-mode entry value. */
  constructor(private readonly target: string) {}

  async fetchSnapshot(signal?: AbortSignal, tracer: FlowTracer = NO_TRACE): Promise<SingleMinerSnapshot> {
    // The adapter layer: the view's generic "get snapshot" maps onto the fleet
    // backend's RPC. Shown in S3 (fleet is one adapter among three); suppressed
    // in S1, where the view is framed as reading the fleet proto directly.
    tracer.adapter("Adapter → fleet RPC", "generic snapshot getter mapped to ListMinerStateSnapshots (by IP)");

    const req = tracer.request("fleet", "ListMinerStateSnapshots", `Connect RPC · ipCidrs=[${this.target}/32]`);
    let res;
    try {
      res = await fleetManagementClient.listMinerStateSnapshots(
        { filter: { ipCidrs: [this.target] }, pageSize: 1 },
        { signal },
      );
      req.ok(`${res.miners.length} miner(s)`);
    } catch (e) {
      req.fail(e instanceof Error ? e.message : String(e));
      throw e;
    }
    const m = res.miners[0];
    if (!m) throw new Error(`No fleet miner found at ${this.target}`);

    // Fleet-native has no adapter: the view is built directly on the fleet proto.
    // Per-ASIC data isn't in the snapshot today, but in production the collector
    // would persist it — so for the demo we treat the grid as fleet-sourced and
    // don't call out the gap in the flow narration.
    tracer.note(
      "fleet-miner",
      "Fleet server ⇽ miner",
      "device telemetry collected out-of-band; the RPC reads the stored snapshot",
    );

    const hashrateThs = current(m.hashrate);
    const tempC = current(m.temperature);
    const powerKw = current(m.powerUsage);

    return {
      identity: {
        name: m.name || m.deviceIdentifier,
        model: m.model || m.driverName || "—",
        firmware: m.firmwareVersion || "—",
        mdkVersion: "via fleet",
        macAddress: m.macAddress || "—",
        serialNumber: m.serialNumber || "—",
        ipAddress: m.ipAddress || this.target,
      },
      status: mapStatus(m),
      kpis: {
        hashrateThs,
        tempC,
        powerW: powerKw === null ? null : powerKw * 1000, // snapshot power is kW
      },
      hashboards: synthGrid(tempC, hashrateThs),
      dataPath: [
        { label: "ProtoFleet client", detail: "React" },
        { label: "Fleet server", detail: "ListMinerStateSnapshots (Connect)" },
        { label: "TimescaleDB", detail: "device-level scalars" },
        { label: "ASIC grid *", detail: "FPO — placeholder grid" },
      ],
      dataPathNote:
        "* For-placement-only: the ASIC grid is faked from the device temperature. Fleet stores only device-level scalars, so going fleet-native for real requires adding per-ASIC collection + persistence (or an on-demand metrics RPC) on the server.",
      source: this.source,
      updatedAt: new Date().toISOString(),
    };
  }
}
