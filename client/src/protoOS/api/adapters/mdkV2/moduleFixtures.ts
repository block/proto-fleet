import { V2MiningState } from "./generated/generatedApi";
import { toCanonicalContainer, toCanonicalHashboardStatus } from "@/protoOS/api/adapters/mdkV2/normalize";
import { asAsicId } from "@/protoOS/store/contract";

export type ModuleFixtureState = "healthy" | "attention" | "missingBoard" | "noPsu" | "partialPsu";

export const moduleFixture = (state: ModuleFixtureState = "healthy") => {
  const fixture = toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [1, 2].map((module) => ({
              id: module,
              slots: [1, 2, 3].map((slot) => ({
                hashboard:
                  state === "missingBoard" && slot === 2
                    ? undefined
                    : {
                        serial: `T1-M${module}-HB${slot}`,
                        location: { hashboardPosition: slot },
                        miningState: V2MiningState.MINING_STATE_MINING,
                        psuIdentity:
                          state === "noPsu"
                            ? undefined
                            : {
                                serial: `PSU-${module}`,
                                model: "CT1 PSU",
                                vendor: "Example vendor",
                                revision: "R2",
                                firmwareVersion: "1.2.3",
                              },
                        health: {
                          timestamp: new Date().toISOString(),
                          hashrate: 88.4,
                          power: 1370.2,
                          averageAsicTemperatureC: 32,
                          errors: state === "attention" && slot === 3 ? [{}] : [],
                          psuElectrical:
                            state === "noPsu"
                              ? undefined
                              : state === "partialPsu"
                                ? { inputPower: 0 }
                                : {
                                    inputVoltage: 415,
                                    inputCurrent: 10.3,
                                    inputPower: 4282,
                                    outputVoltage: 12,
                                    outputCurrent: 342.6,
                                    outputPower: 4110.6,
                                  },
                          psuThermal:
                            state === "noPsu" || state === "partialPsu"
                              ? undefined
                              : {
                                  thermalStateValid: true,
                                  overTemperatureWarning: state === "attention",
                                  inletTemperature: 50,
                                  srHeatsinkTemperature: 58,
                                  pfcHeatsinkTemperature: 55,
                                },
                        },
                      },
              })),
            })),
          },
        ],
      },
    },
    {},
    {},
    "ct1-story",
  );
  for (const board of fixture.hardware.topology!.hashboards.values()) {
    const samples = Array.from({ length: 312 }, (_, index) => ({
      index,
      temperatureDc: state === "attention" && board.location?.slot === 3 && index > 135 && index < 155 ? 850 : 320,
    }));
    const status = toCanonicalHashboardStatus(board.identity.value, {
      layout: {
        stackCount: 4,
        asicsPerStack: 78,
        entries: samples.map(({ index }) => ({
          index,
          stackId: Math.floor(index / 78),
          stackIndex: index % 78,
          x: index % 78,
          y: Math.floor(index / 78),
        })),
      },
      asics: samples,
    });
    board.asicIds = status.asicIds.map(asAsicId);
    for (const asic of status.asics) {
      const id = asAsicId(asic.id);
      fixture.hardware.asics.set(id, {
        ref: { id, kind: "asic", owner: { kind: "hashboard", id: board.id } },
        hashboardId: board.id,
        index: asic.index,
        row: asic.row,
        column: asic.column,
      });
    }
    for (const sample of status.asicTelemetry) {
      const asicId = asAsicId(sample.asicId);
      fixture.telemetry.asics.set(asicId, { asicId, temperature: { latest: sample.temperature } });
    }
    fixture.telemetry.hashboards.get(board.id)!.maximumAsicTemperature = {
      latest: { value: Math.max(...samples.map((sample) => sample.temperatureDc / 10)), units: "°C" },
    };
  }
  return fixture;
};
