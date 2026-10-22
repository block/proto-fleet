import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { ModuleIssues } from "../ModuleIssues";
import ModuleHashboardCard from "./ModuleHashboardCard";
import ModulePsuCard from "./ModulePsuCard";
import {
  EntityKind,
  HashboardSlotSchema,
  InventoryItemSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const settings = vi.hoisted(() => ({ unit: "C" }));
vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => settings.unit }));
beforeEach(() => {
  settings.unit = "C";
});
function fixture() {
  const now = Date.now();
  const observedAt = timestampFromMs(now);
  const module = create(InventoryItemSchema, {
    ref: { kind: EntityKind.MODULE, id: 91n },
    moduleIssues: {
      observedAt,
      complete: true,
      items: [
        { component: "psu", cause: "PSU_OVER_TEMPERATURE_WARNING", severity: "warning", observedAt },
        { component: "hashboard", cause: "HASHBOARD_ERROR_CAUSE_ASIC_ENUMERATION", severity: "fault", observedAt },
      ],
    },
  });
  const slots = [1, 2, 3].map((position) =>
    create(HashboardSlotSchema, {
      position,
      observedAt,
      occupancy: "occupied",
      board: {
        moduleId: 91n,
        observedAt,
        metadata: { psuIdentity: { serial: "PSU-ONE", vendor: "BOCO", model: "MODEL-ONE", firmwareVersion: "1.2.3" } },
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
    }),
  );
  return { module, slots, now, unavailable: false };
}
it("shows one shared PSU with the ProtoOS metrics and opens only PSU details from info", () => {
  render(<ModulePsuCard {...fixture()} />);
  expect(screen.getAllByRole("article", { name: "PSU 1" })).toHaveLength(1);
  for (const [label, value] of [
    ["Input voltage", "415.0 V"],
    ["Input power", "18.0 kW"],
    ["Output voltage", "48.0 V"],
    ["Output power", "17.4 kW"],
    ["Avg temp", "40.0 °C"],
    ["High temp", "50.0 °C"],
  ]) {
    expect(screen.getByText(label).parentElement).toHaveTextContent(value);
  }
  expect(screen.queryByText("PSU-ONE")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("img", { name: "PSU 1 needs attention" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  const info = screen.getByRole("button", { name: "PSU 1 info" });
  info.focus();
  fireEvent.click(info);
  const dialog = screen.getByRole("dialog", { name: "PSU 1 status" });
  expect(dialog).toHaveTextContent("PSU · Over temperature warning");
  expect(dialog).toHaveTextContent("PSU-ONE");
  expect(dialog).toHaveTextContent("BOCO");
  expect(dialog).toHaveTextContent("MODEL-ONE");
  expect(dialog).toHaveTextContent("1.2.3");
  expect(dialog).not.toHaveTextContent("Asic enumeration");
  fireEvent.click(within(dialog).getByRole("button", { name: "Done" }));
  expect(info).toHaveFocus();
});
it("uses Fleet temperature preferences", () => {
  settings.unit = "F";
  render(<ModulePsuCard {...fixture()} />);
  expect(screen.getByText("Avg temp").parentElement).toHaveTextContent("104.0 °F");
  expect(screen.getByText("High temp").parentElement).toHaveTextContent("122.0 °F");
});
it("omits healthy issue icons and marks incomplete evidence as unknown", () => {
  const props = fixture();
  props.module.moduleIssues!.items = [];
  props.module.moduleIssues!.complete = false;
  render(<ModulePsuCard {...props} />);
  expect(screen.queryByRole("img", { name: "PSU 1 needs attention" })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "PSU 1 info" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("Status unknown");
});
it("keeps PSU issues available when telemetry is missing and hides old issues", () => {
  const props = fixture();
  props.slots = [];
  const { rerender } = render(<ModulePsuCard {...props} />);
  expect(screen.getByRole("article", { name: "PSU 1" })).toBeInTheDocument();
  expect(screen.getByText("Input power").parentElement).toHaveTextContent("—");
  rerender(<ModulePsuCard {...props} now={props.now + 120000} />);
  expect(screen.getByText("No PSU data available.")).toBeInTheDocument();
});
it("clears PSU readings and identity when container data is unavailable", () => {
  const props = fixture();
  const { rerender } = render(<ModulePsuCard {...props} />);
  rerender(<ModulePsuCard {...props} unavailable />);
  expect(screen.queryByText("18.0 kW")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "PSU 1 info" })).toBeInTheDocument();
  expect(screen.getByText("No PSU data available.")).toBeInTheDocument();
});

it.each(["warning", "fault"])(
  "keeps PSU-only %s causes in PSU details while displaying canonical board status",
  (severity) => {
    const props = fixture();
    props.module.name = "Module 1";
    props.module.moduleIssues!.items = props.module.moduleIssues!.items.filter((issue) => issue.component === "psu");
    props.module.moduleIssues!.items[0].severity = severity;
    props.module.moduleIssues!.items[0].cause = "PSU_OVER_TEMPERATURE";
    for (const slot of props.slots) {
      Object.assign(slot.board!, {
        state: severity === "fault" ? "error" : "active",
        stateObservedAt: timestampFromMs(props.now),
        miningState: "MINING_STATE_MINING",
        hashrailOk: true,
        refclksOk: true,
      });
      Object.assign(slot.board!.health!, {
        errors: [],
        warnings: [],
        psuThermal: {
          thermalStateValid: true,
          thermalStateAgeMs: 100,
          overTemperatureWarning: severity === "warning",
          overTemperatureFault: severity === "fault",
        },
      });
    }
    render(
      <>
        {props.slots.map((slot) => (
          <ModuleHashboardCard key={slot.position} slot={slot} now={props.now} />
        ))}
        <ModulePsuCard {...props} />
        <ModuleIssues item={props.module} status="Needs attention" />
      </>,
    );
    for (const position of [1, 2, 3]) {
      const indicator = screen.queryByRole("img", { name: `Board ${position} needs attention` });
      if (severity === "fault") expect(indicator).toBeInTheDocument();
      else expect(indicator).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: `Board ${position} info` }));
      expect(screen.getByRole("dialog")).not.toHaveTextContent(/Fault reported|Hashboard.*failure/);
      fireEvent.click(screen.getByRole("button", { name: "Done" }));
    }
    expect(screen.getByRole("img", { name: "PSU 1 needs attention" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "PSU 1 info" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("PSU · Over temperature");
    expect(screen.getByRole("dialog")).toHaveTextContent(severity === "warning" ? "Warning" : "Fault");
    expect(screen.getByRole("dialog")).not.toHaveTextContent("Fault reported");
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Module 1 health: Needs attention" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("PSU · Over temperature");
  },
);

it("keeps PSU details open when data expires and restores focus on dismissal", () => {
  const props = fixture();
  const { rerender } = render(<ModulePsuCard {...props} />);
  const info = screen.getByRole("button", { name: "PSU 1 info" });
  info.focus();
  fireEvent.click(info);
  rerender(<ModulePsuCard {...props} now={props.now + 120_000} />);
  expect(screen.getByRole("dialog")).toHaveTextContent("Status unknown");
  expect(screen.getByRole("dialog")).not.toHaveTextContent(/PSU-ONE|Over temperature/);
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getByRole("button", { name: "PSU 1 info" })).toHaveFocus();
});
