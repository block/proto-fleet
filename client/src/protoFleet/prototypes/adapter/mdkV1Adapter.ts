/**
 * MDK v1 adapter — talks to today's Proto REST surface (`/api/v1/*`).
 *
 * The honest part of this prototype: v1 spreads a miner across many endpoints
 * (login, system, mining, hashboards) and exposes NO bulk per-chip data. So the
 * ASIC mini-grid here is *synthesized* from each board's ASIC count around its
 * average ASIC temperature — a faithful reflection of what a v1-only client can
 * actually show without N-per-chip round trips. Contrast MdkV2Adapter, which
 * gets real per-chip readings in one call.
 */
import type { SingleMinerAdapter } from "../shared/adapter";
import type {
  AsicCell,
  AsicHealth,
  HashboardSummary,
  MinerControlAction,
  MinerStatus,
  SingleMinerSnapshot,
} from "../shared/types";
import { getJson, hostOf, postJson } from "./http";

interface V1Login {
  access_token: string;
  refresh_token: string;
}
interface V1System {
  "system-info": {
    model?: string;
    cb_sn: string;
    os: { name: string; version: string; hostname: string };
  };
}
interface V1Mining {
  "mining-status": {
    status: string;
    hashrate_ghs: number;
    power_usage_watts: number;
    average_hb_temp_c: number;
    average_asic_temp_c: number;
    hashboards_installed: number;
  };
}
interface V1Hashboards {
  "hashboards-info": Array<{
    slot: number;
    hb_sn?: string;
    mining_asic_count?: number;
  }>;
}

function mapStatus(raw: string): MinerStatus {
  switch (raw) {
    case "Mining":
      return "mining";
    case "DegradedMining":
      return "error";
    case "Stopped":
    case "NoPools":
    case "PoweringOn":
    case "PoweringOff":
    case "Curtailed":
      return "paused";
    default:
      return "offline";
  }
}

/** Synthesize a per-chip grid around the board's average ASIC temp. */
function synthAsics(count: number, avgTempC: number, seed: number): AsicCell[] {
  return Array.from({ length: count }, (_, index) => {
    const wobble = ((index * 7 + seed * 13) % 11) - 5;
    const tempC = avgTempC + wobble;
    let health: AsicHealth = "ok";
    if (tempC >= avgTempC + 4) health = "warn";
    if (tempC >= avgTempC + 8) health = "error";
    if ((index * 3 + seed) % 41 === 0) health = "off";
    return {
      index,
      tempC: health === "off" ? null : Math.round(tempC * 10) / 10,
      hashrateThs: health === "off" ? 0 : Math.round((1.1 + wobble / 50) * 100) / 100,
      health,
    };
  });
}

export class MdkV1Adapter implements SingleMinerAdapter {
  readonly source = "MDK v1 REST (direct)";
  private token: string | null = null;

  constructor(
    private readonly baseUrl: string,
    private readonly password: string,
  ) {}

  private async ensureToken(signal?: AbortSignal): Promise<string> {
    if (this.token) return this.token;
    const res = await postJson<V1Login>(`${this.baseUrl}/api/v1/auth/login`, { password: this.password }, { signal });
    this.token = res.access_token;
    return this.token;
  }

  async fetchSnapshot(signal?: AbortSignal): Promise<SingleMinerSnapshot> {
    const token = await this.ensureToken(signal);

    // v1 requires several round trips — a real cost the data-path ribbon shows.
    const [system, mining, boards] = await Promise.all([
      getJson<V1System>(`${this.baseUrl}/api/v1/system`, { signal, token }),
      getJson<V1Mining>(`${this.baseUrl}/api/v1/mining`, { signal, token }),
      getJson<V1Hashboards>(`${this.baseUrl}/api/v1/hashboards`, { signal, token }),
    ]);

    const s = system["system-info"];
    const m = mining["mining-status"];
    const boardList = boards["hashboards-info"] ?? [];
    const perBoardThs = boardList.length ? m.hashrate_ghs / 1000 / boardList.length : 0;

    const hashboards: HashboardSummary[] = boardList.map((b, i) => {
      const asics = synthAsics(b.mining_asic_count ?? 0, m.average_asic_temp_c, b.slot || i + 1);
      const live = asics.filter((a) => a.health !== "off");
      return {
        serialNumber: b.hb_sn ?? `HB-${b.slot}`,
        index: b.slot,
        tempC: live.length ? Math.max(...live.map((a) => a.tempC ?? 0)) : null,
        hashrateThs: Math.round(perBoardThs * 100) / 100,
        asics,
      };
    });

    return {
      identity: {
        name: s.os.hostname || s.cb_sn,
        model: s.model ?? "Proto",
        firmware: s.os.version,
        mdkVersion: "MDK v1",
        macAddress: "—", // v1 /system does not expose MAC
        serialNumber: s.cb_sn,
        ipAddress: hostOf(this.baseUrl),
      },
      status: mapStatus(m.status),
      kpis: {
        hashrateThs: m.hashrate_ghs / 1000,
        tempC: m.average_hb_temp_c,
        powerW: m.power_usage_watts,
      },
      hashboards,
      dataPath: [
        { label: "Browser", detail: "ProtoFleet client" },
        { label: "MDK v1 REST", detail: "login + system + mining + hashboards" },
        { label: "Adapter", detail: "maps + synthesizes ASIC grid" },
      ],
      source: this.source,
      updatedAt: new Date().toISOString(),
    };
  }

  async control(action: MinerControlAction): Promise<void> {
    const token = await this.ensureToken();
    const path =
      action === "reboot"
        ? "/api/v1/system/reboot"
        : action === "pause"
          ? "/api/v1/mining/stop"
          : "/api/v1/mining/start";
    await postJson(`${this.baseUrl}${path}`, {}, { token });
  }
}
