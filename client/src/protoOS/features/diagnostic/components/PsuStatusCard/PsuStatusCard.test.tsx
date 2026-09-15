import { act, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import PsuStatusCard from "./PsuStatusCard";
import { asComponentId, asModuleId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

vi.mock("@/protoOS/components/StatusModal", () => ({ ProtoOSStatusModal: () => null }));
const ids = [asComponentId("module:a:psu:1"), asComponentId("module:b:psu:1")];

beforeEach(() => {
  useMinerStore.getState().resetDeviceData();
  useMinerStore.setState((state) => {
    ids.forEach((id) =>
      state.hardware.psus.set(id, {
        ref: { id, kind: "psu", owner: { kind: "module", id: asModuleId(id.slice(0, -6)) } },
        slot: 1,
      }),
    );
    state.telemetry.psus.set(ids[0], {
      componentId: ids[0],
      health: "fault",
      inputPower: { latest: { value: 0, units: "W" } },
    });
    state.telemetry.psus.set(ids[1], { componentId: ids[1], health: "ok" });
  });
});

test("shows missing readings as dashes, retains real zero, and isolates PSU warnings", () => {
  render(
    <>
      <PsuStatusCard componentId={ids[0]} />
      <PsuStatusCard componentId={ids[1]} />
    </>,
  );
  const [first, second] = screen.getAllByTestId("card").map((card) => within(card));
  expect(first.getAllByText("—")).toHaveLength(5);
  expect(first.getByText("0.0 kW")).toBeInTheDocument();
  expect(first.getByRole("img", { name: "PSU 1 needs attention" })).toBeInTheDocument();
  expect(second.getAllByText("—")).toHaveLength(6);
  expect(second.queryByRole("img")).not.toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: "More info" })).toHaveLength(2);
  act(() =>
    useMinerStore.setState((state) => {
      state.telemetry.psus.get(ids[0])!.health = "ok";
    }),
  );
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
});
