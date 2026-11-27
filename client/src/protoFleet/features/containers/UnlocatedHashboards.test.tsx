import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import UnlocatedHashboards from "./UnlocatedHashboards";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

it("keeps retained board details accessible when the reported count is zero", () => {
  render(
    <UnlocatedHashboards
      container={create(ContainerSchema, {
        unlocatedHashboards: 0,
        unlocatedBoards: [{ serial: "unlocated-1", state: "active" }],
      })}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
  expect(screen.getByRole("button", { name: "View hashboard unlocated-1" })).toBeInTheDocument();
});

it("uses the reported count when details are unavailable", () => {
  render(<UnlocatedHashboards container={create(ContainerSchema, { unlocatedHashboards: 4 })} />);
  expect(screen.getByText("Unlocated hashboards (4)")).toBeInTheDocument();
});
