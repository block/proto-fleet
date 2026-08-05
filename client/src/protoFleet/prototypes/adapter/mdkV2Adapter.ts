/**
 * MDK v2 adapter — talks to the (faked) consolidated `GET /api/v2/miner`.
 *
 * v2 is deliberately divergent from v1: one call returns a wrapped envelope with
 * camelCase fields, hashrate in GH/s, nested thermals, and a real per-chip
 * `chips[]` array with a state enum. The adapter's job is to fold that different
 * shape into the exact same SingleMinerSnapshot — no synthesis needed, because
 * v2 actually reports per-chip data. This is the payoff of the adapter seam:
 * two very different wire formats, one view.
 */
import type { SingleMinerAdapter } from "../shared/adapter";
import type { AsicHealth, HashboardSummary, MinerStatus, SingleMinerSnapshot } from "../shared/types";
import { getJson, hostOf } from "./http";

interface V2Chip {
  pos: number;
  tempC: number;
  ghs: number;
  state: "ONLINE" | "HOT" | "FAULT" | "OFFLINE" | string;
}
interface V2Board {
  slot: number;
  serial: string;
  hashrateGhs: number;
  thermals: { peakC: number; avgC: number };
  chips: V2Chip[];
}
interface V2Envelope {
  apiVersion: string;
  data: {
    device: {
      displayName: string;
      hardwareModel: string;
      firmwareRev: string;
      mdk: string;
      netMac: string;
      unitSerial: string;
      lanIp: string;
    };
    state: string;
    performance: {
      hashrateGhs: number;
      powerWatts: number;
      thermals: { peakC: number; avgC: number };
    };
    boards: V2Board[];
  };
  meta: { generatedAt: string; schema: string };
}

function mapState(state: string): MinerStatus {
  switch (state) {
    case "HASHING":
      return "mining";
    case "IDLE":
      return "paused";
    case "FAULT":
      return "error";
    default:
      return "offline";
  }
}

function mapChipHealth(state: string): AsicHealth {
  switch (state) {
    case "ONLINE":
      return "ok";
    case "HOT":
      return "warn";
    case "FAULT":
      return "error";
    case "OFFLINE":
      return "off";
    default:
      return "off";
  }
}

export class MdkV2Adapter implements SingleMinerAdapter {
  readonly source = "MDK v2 consolidated (direct)";

  constructor(private readonly baseUrl: string) {}

  async fetchSnapshot(signal?: AbortSignal): Promise<SingleMinerSnapshot> {
    const env = await getJson<V2Envelope>(`${this.baseUrl}/api/v2/miner`, { signal });
    const d = env.data;

    const hashboards: HashboardSummary[] = d.boards.map((b) => ({
      serialNumber: b.serial,
      index: b.slot,
      tempC: b.thermals.peakC,
      hashrateThs: Math.round((b.hashrateGhs / 1000) * 100) / 100,
      asics: b.chips.map((c) => ({
        index: c.pos,
        tempC: c.state === "OFFLINE" ? null : c.tempC,
        hashrateThs: Math.round((c.ghs / 1000) * 1000) / 1000,
        health: mapChipHealth(c.state),
      })),
    }));

    return {
      identity: {
        name: d.device.displayName,
        model: d.device.hardwareModel,
        firmware: d.device.firmwareRev,
        mdkVersion: `MDK v${d.device.mdk}`,
        macAddress: d.device.netMac,
        serialNumber: d.device.unitSerial,
        ipAddress: d.device.lanIp || hostOf(this.baseUrl),
      },
      status: mapState(d.state),
      kpis: {
        hashrateThs: d.performance.hashrateGhs / 1000,
        tempC: d.performance.thermals.peakC,
        powerW: d.performance.powerWatts,
      },
      hashboards,
      dataPath: [
        { label: "Browser", detail: "ProtoFleet client" },
        { label: "MDK v2", detail: "GET /api/v2/miner (consolidated)" },
        { label: "Adapter", detail: "folds envelope → snapshot" },
      ],
      source: this.source,
      updatedAt: env.meta.generatedAt,
    };
  }
}
