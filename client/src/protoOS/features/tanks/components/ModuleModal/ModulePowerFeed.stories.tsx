import { useMemo } from "react";
import ModulePowerFeed from "./ModulePowerFeed";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";
import type { ModuleNode, TankNode } from "@/protoOS/store/contract";

const FEED = "pdu-1-outlet-05";
const ENABLED = "POWER_FEED_STATE_ENABLED";
const DISABLED = "POWER_FEED_STATE_DISABLED";
// A de-energized outlet reads zero on every phase.
const OFF = {
  active: false,
  voltageV: 0,
  currentA: 0,
  powerW: 0,
  voltageAbV: 0,
  voltageBcV: 0,
  voltageCaV: 0,
  currentAA: 0,
  currentBA: 0,
  currentCA: 0,
};
const tank = { id: "ct1:tank:1", slot: 1, moduleIds: [] } as unknown as TankNode;
const module = { id: "ct1:module:M-1-5", location: { tankLabel: "Tank 1", moduleSlot: 5 } } as unknown as ModuleNode;

type Scenario = {
  wiring?: string;
  /** Another module whose boards also name this feed. */
  shared?: boolean;
  feed?: Record<string, unknown>;
  snapshotReady?: boolean;
  cycle?: boolean;
  mapFails?: boolean;
  hosted?: boolean;
};

const slot = (position: number, wiring?: string, moduleId = "M-1-5") => ({
  occupied: true,
  powerFeedId: FEED,
  hashboard: {
    macAddress: `aa:bb:cc:00:05:0${position}`,
    moduleId,
    location: { hashboardPosition: position, powerFeedId: FEED, wiringStatus: wiring },
  },
});

// A controller that answers the three reads one inspection takes.
function previewAdapter({ wiring, shared, feed, snapshotReady = true, cycle, mapFails }: Scenario) {
  const client = {
    hashboardServiceGetContainerMap: async () => {
      if (mapFails) throw new Error("The controller map could not be read.");
      return {
        data: {
          containerMap: {
            tanks: [
              {
                id: 1,
                modules: [
                  {
                    id: 5,
                    slots: [1, 2, 3].map((position) => slot(position, position === 2 ? wiring : wiring && "verified")),
                  },
                  ...(shared ? [{ id: 6, slots: [slot(1, undefined, "M-1-6")] }] : []),
                ],
              },
            ],
          },
        },
      };
    },
    powerServiceListPowerFeeds: async () => ({
      data: {
        snapshotReady,
        powerFeeds: [
          {
            powerFeedId: FEED,
            state: ENABLED,
            active: true,
            faulted: false,
            overridden: false,
            voltageV: 480.2,
            currentA: 21.9,
            powerW: 18_240,
            voltageAbV: 480.2,
            voltageBcV: 479.8,
            voltageCaV: 480.4,
            currentAA: 21.9,
            currentBA: 21.8,
            currentCA: 22,
            ...feed,
          },
        ],
      },
    }),
    powerServiceGetActivePowerRecoveryCycle: async () => ({
      data: cycle
        ? {
            cycle: {
              id: "cycle-1",
              phase: "holding",
              outletIds: [FEED],
              restoreAfter: new Date(Date.now() + 25_000).toISOString(),
            },
          }
        : {},
    }),
  };
  return new MdkV2Adapter(client as unknown as MdkV2Client, { deviceId: "ct1-story" });
}

function Preview({ hosted, ...scenario }: Scenario) {
  const { wiring, shared, feed, snapshotReady, cycle, mapFails } = scenario;
  const adapter = useMemo(
    () => previewAdapter({ wiring, shared, feed, snapshotReady, cycle, mapFails }),
    [wiring, shared, feed, snapshotReady, cycle, mapFails],
  );
  return (
    <MinerHostingContext.Provider
      value={{
        adapter,
        resolution: { status: "ready", adapterId: "mdk-v2", adapter },
        minerRoot: "",
        closeButton: null,
        mode: hosted ? "fleet" : "direct",
        metadata: {},
      }}
    >
      <div className="max-w-6xl bg-surface-base p-6 text-text-primary">
        <ModulePowerFeed module={module} tank={tank} />
      </div>
    </MinerHostingContext.Provider>
  );
}

export const Configured = () => <Preview />;
export const WiringVerified = () => <Preview wiring="verified" />;
export const Ambiguous = () => <Preview shared wiring="mismatch" />;
export const OverriddenOff = () => <Preview feed={{ ...OFF, state: DISABLED, overridden: true }} />;
export const ReadingsNotReady = () => <Preview snapshotReady={false} />;
export const RecoveryCycle = () => <Preview cycle feed={{ ...OFF, state: DISABLED }} />;
export const MapUnavailable = () => <Preview mapFails />;
export const FleetHosted = () => <Preview hosted />;

export default { title: "Proto OS/Containers/Module power feed" };
