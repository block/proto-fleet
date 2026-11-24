import { useMemo } from "react";
import ModuleVoltageSection from "./ModuleVoltageSection";
import type {
  ModuleVoltageBoardResult,
  ModuleVoltageSetting,
  ModuleVoltageWriteResult,
} from "@/protoOS/api/adapters/moduleVoltage";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";
import type { ModuleNode } from "@/protoOS/store/contract";

const WIRE_ID = "0011223344556677";
const module = {
  id: `ct1-story:module:${WIRE_ID}`,
  hashboardIds: [],
  psuIds: [],
  controlBoardIds: [],
  fanIds: [],
} as unknown as ModuleNode;
const range = { minimumMillivolts: 26_000, maximumMillivolts: 30_000 };
const boards = (unapplied = 0): ModuleVoltageBoardResult[] =>
  [1, 2, 3].map((slot) => ({
    macAddress: `aa:bb:cc:00:00:0${slot}`,
    applied: slot > unapplied,
    ...(slot > unapplied ? {} : { message: "hashboard did not acknowledge the voltage command" }),
  }));

/** Renders the section against an in-memory controller. Use the buttons to see each write outcome. */
const VoltageStory = ({
  initial,
  write = "applied",
}: {
  initial: Partial<ModuleVoltageSetting>;
  write?: "applied" | "partial" | "refused";
}) => {
  const adapter = useMemo(() => {
    let saved: ModuleVoltageSetting = {
      moduleId: WIRE_ID,
      supportedRange: range,
      boardCount: 3,
      readiness: "ready",
      updatedBy: "proto-os",
      updatedAt: new Date("2026-01-15T10:00:00Z"),
      ...initial,
    };
    const save = async (fixedMillivolts?: number): Promise<ModuleVoltageWriteResult> => {
      if (write === "refused") {
        throw Object.assign(
          new Error(
            "The controller refused the request: this node is not the container control primary. The voltage setting was not changed.",
          ),
          { writeOutcome: "refused" },
        );
      }
      const { fixedMillivolts: _previous, ...rest } = saved;
      saved = { ...rest, ...(fixedMillivolts === undefined ? {} : { fixedMillivolts }), updatedAt: new Date() };
      const results = boards(write === "partial" ? 1 : 0);
      return { boardsUpdated: results.filter((board) => board.applied).length, boards: results };
    };
    return {
      listModuleVoltageSettings: async () => [saved],
      setModuleFixedVoltage: (_moduleId: string, millivolts: number) => save(millivolts),
      clearModuleFixedVoltage: () => save(),
    } as unknown as MinerAdapter;
  }, [initial, write]);
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
      <div className="mx-auto max-w-240 p-6 text-text-primary">
        <ModuleVoltageSection module={module} />
      </div>
    </MinerHostingContext.Provider>
  );
};

const AUTOMATIC = {};
const FIXED = { fixedMillivolts: 28_000 };
const RANGE_UNAVAILABLE = {
  fixedMillivolts: 28_000,
  supportedRange: undefined,
  readiness: "limitsUnavailable" as const,
};

export const Automatic = () => <VoltageStory initial={AUTOMATIC} />;
export const Fixed = () => <VoltageStory initial={FIXED} />;
export const RangeUnavailable = () => <VoltageStory initial={RANGE_UNAVAILABLE} />;
/** Set a voltage to see one hashboard reported as not applied yet. */
export const PartiallyApplied = () => <VoltageStory initial={AUTOMATIC} write="partial" />;
/** Set a voltage to see the controller refuse it. */
export const Refused = () => <VoltageStory initial={AUTOMATIC} write="refused" />;

export default { title: "Proto OS/Containers/Module voltage" };
