import { expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { modulePsu } from "./modulePsu";
import { HashboardSlotSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const now = Date.now();
function slot(position = 1, observed = now) {
  return create(HashboardSlotSchema, {
    position,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      moduleId: 91n,
      observedAt: timestampFromMs(observed),
      metadata: { psuIdentity: { serial: "PSU-ONE", vendor: "BOCO" } },
      health: {
        psuElectrical: {
          inputVoltage: 415,
          inputPower: 18000,
          outputVoltage: 48,
          outputPower: 17400,
          sampleAgeMs: 100,
        },
        psuThermal: {
          inletTemperature: 30,
          srHeatsinkTemperature: 50,
          pfcHeatsinkTemperature: 40,
          temperaturesAgeMs: 200,
        },
      },
    },
  });
}
it("deduplicates the module PSU and derives average and high temperatures from PSU sensors", () => {
  expect(modulePsu([slot(1), slot(2), slot(3)], 91n, now)).toMatchObject({
    identity: { serial: "PSU-ONE", vendor: "BOCO" },
    inputVoltage: 415,
    inputPower: 18000,
    outputVoltage: 48,
    outputPower: 17400,
    averageTemperature: 40,
    highTemperature: 50,
  });
});
it("selects each complete snapshot by source time minus sample age, never sums duplicate PSU power", () => {
  const newer = slot(1);
  const older = slot(2, now - 5000);
  older.board!.health!.psuElectrical = { inputPower: 9000, sampleAgeMs: 0 };
  newer.board!.health!.psuThermal = { inletTemperature: 10, temperaturesAgeMs: 10000 };
  const result = modulePsu([older, newer], 91n, now);
  expect(result?.inputPower).toBe(18000);
  expect(result?.averageTemperature).toBe(40);
});
it("expires embedded samples including elapsed collection time independently", () => {
  const value = slot(1, now - 60000);
  value.board!.health!.psuElectrical = { inputPower: 18000, sampleAgeMs: 61000 };
  const result = modulePsu([value], 91n, now);
  expect(result?.inputPower).toBeUndefined();
  expect(result?.averageTemperature).toBe(40);
});
it.each([-1, "10", NaN, Infinity, 120000])("hides electrical data with invalid age %s", (age) => {
  const value = slot();
  value.board!.health!.psuElectrical = { inputPower: 18000, ...(age === undefined ? {} : { sampleAgeMs: age }) };
  expect(modulePsu([value], 91n, now)?.inputPower).toBeUndefined();
});
it("keeps zero readings, omits malformed numbers, and does not use board temperatures", () => {
  const value = slot();
  value.board!.health!.psuElectrical = { inputPower: 0, outputPower: "17400", sampleAgeMs: 0 };
  value.board!.health!.psuThermal = {
    inletTemperature: 0,
    srHeatsinkTemperature: "50",
    pfcHeatsinkTemperature: null,
    temperaturesAgeMs: 0,
  };
  expect(modulePsu([value], 91n, now)).toMatchObject({ inputPower: 0, averageTemperature: 0, highTemperature: 0 });
  expect(modulePsu([value], 91n, now)?.outputPower).toBeUndefined();
});
it("ignores unknown occupancy, other modules, expired and future board evidence", () => {
  const unknown = slot();
  unknown.occupancy = "unknown";
  const wrongModule = slot();
  wrongModule.board!.moduleId = 92n;
  const staleSlot = slot();
  staleSlot.observedAt = timestampFromMs(now - 120000);
  expect(modulePsu([unknown, wrongModule, staleSlot], 91n, now)).toBeUndefined();
  expect(modulePsu([slot(1, now - 120000)], 91n, now)?.inputPower).toBeUndefined();
  expect(modulePsu([slot(1, now + 6000)], 91n, now)?.inputPower).toBeUndefined();
});
it("does not invent PSU data from board power alone", () => {
  const value = slot();
  value.board!.metadata = {};
  value.board!.health = { power: 6000 };
  expect(modulePsu([value], 91n, now)).toBeUndefined();
});

it("uses the health observation time when optional PSU sample ages are omitted", () => {
  const value = slot();
  value.board!.health!.psuElectrical = { inputPower: 18000 };
  value.board!.health!.psuThermal = { inletTemperature: 30, srHeatsinkTemperature: 50 };
  expect(modulePsu([value], 91n, now)).toMatchObject({
    inputPower: 18000,
    averageTemperature: 40,
    highTemperature: 50,
  });
  expect(modulePsu([value], 91n, now + 120_000)).toBeUndefined();
});
