import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { canaryChannel } from "./ReleaseChannels.fixtures";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import { ReleaseChannelModelGroupSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

afterEach(() => vi.restoreAllMocks());

describe("release channel model row identity", () => {
  it.each([
    {
      name: "colon-containing identities",
      first: { manufacturer: "a:b", model: "c" },
      second: { manufacturer: "a", model: "b:c" },
    },
    {
      name: "distinct raw case variants",
      first: { manufacturer: "PROTO", model: "Rig" },
      second: { manufacturer: "proto", model: "rig" },
    },
  ])("preserves the right rows when $name reorder, update, and disappear", ({ first, second }) => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const firstGroup = create(ReleaseChannelModelGroupSchema, { ...first, minerCount: 2 });
    const secondGroup = create(ReleaseChannelModelGroupSchema, { ...second, minerCount: 3 });
    const channel = { ...canaryChannel, minerCount: 5, modelGroups: [firstGroup, secondGroup] };
    const props = { channels: [channel], rollouts: [], onCreate: vi.fn(), onManage: vi.fn() };
    const { rerender } = render(<ReleaseChannelsTable {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
    const firstRow = screen.getByTestId(`model-row-Canary-${first.model}`).closest("tr")!;
    const secondRow = screen.getByTestId(`model-row-Canary-${second.model}`).closest("tr")!;
    expect(within(firstRow).getAllByRole("cell")[1]).toHaveTextContent(/^2$/);
    expect(within(secondRow).getAllByRole("cell")[1]).toHaveTextContent(/^3$/);

    rerender(
      <ReleaseChannelsTable
        {...props}
        channels={[{ ...channel, minerCount: 6, modelGroups: [{ ...secondGroup, minerCount: 4 }, firstGroup] }]}
      />,
    );
    expect(screen.getByTestId(`model-row-Canary-${first.model}`).closest("tr")).toBe(firstRow);
    expect(screen.getByTestId(`model-row-Canary-${second.model}`).closest("tr")).toBe(secondRow);
    expect(within(secondRow).getAllByRole("cell")[1]).toHaveTextContent(/^4$/);

    rerender(
      <ReleaseChannelsTable
        {...props}
        channels={[{ ...channel, minerCount: 4, modelGroups: [{ ...secondGroup, minerCount: 4 }] }]}
      />,
    );
    expect(screen.queryByTestId(`model-row-Canary-${first.model}`)).not.toBeInTheDocument();
    expect(firstRow).not.toBeInTheDocument();
    expect(screen.getByTestId(`model-row-Canary-${second.model}`).closest("tr")).toBe(secondRow);
    expect(screen.getAllByTestId("list-row")).toHaveLength(2);
    expect(errors).not.toHaveBeenCalled();
  });
});
