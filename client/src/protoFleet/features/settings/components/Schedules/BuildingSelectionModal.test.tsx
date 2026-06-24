import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import BuildingSelectionModal from "./BuildingSelectionModal";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";

const { listBuildingsMock, pushToastMock } = vi.hoisted(() => ({
  listBuildingsMock: vi.fn(),
  pushToastMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listBuildings: listBuildingsMock }),
}));

vi.mock("@/shared/components/Modal", () => ({
  __esModule: true,
  default: ({
    children,
    buttons,
    title,
  }: {
    children: ReactNode;
    buttons?: Array<{ text: string; onClick?: () => void }>;
    title?: string;
  }) => (
    <div>
      <div>{title}</div>
      {children}
      {buttons?.map((button) => (
        <button key={button.text} type="button" onClick={button.onClick}>
          {button.text}
        </button>
      ))}
    </div>
  ),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: (...args: unknown[]) => pushToastMock(...args),
  STATUSES: { error: "error" },
}));

const createBuilding = (id: bigint, name: string) =>
  create(BuildingWithCountsSchema, { building: create(BuildingSchema, { id, name }) });

type Callbacks = {
  siteIds?: bigint[];
  includeUnassigned?: boolean;
  onSuccess?: (rows: ReturnType<typeof createBuilding>[]) => void;
  onFinally?: () => void;
};

describe("BuildingSelectionModal", () => {
  beforeEach(() => {
    listBuildingsMock.mockReset();
    pushToastMock.mockReset();
  });

  it("lists every building with the all-sites filter (no regression)", async () => {
    listBuildingsMock.mockImplementation(({ siteIds, onSuccess, onFinally }: Callbacks) => {
      expect(siteIds).toEqual([]);
      onSuccess?.([createBuilding(1n, "Building A"), createBuilding(2n, "Building B")]);
      onFinally?.();
    });

    render(
      <BuildingSelectionModal
        open
        selectedBuildingIds={[]}
        scope={{ siteIds: [], includeUnassigned: false }}
        onDismiss={vi.fn()}
        onSave={vi.fn()}
      />,
    );

    await waitFor(() => expect(screen.getByText("Building A")).toBeVisible());
    expect(screen.getByText("Building B")).toBeVisible();
    expect(listBuildingsMock).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [], includeUnassigned: false }));
  });

  it("filters buildings to the selected site and preserves off-site selections", async () => {
    listBuildingsMock.mockImplementation(({ siteIds, onSuccess, onFinally }: Callbacks) => {
      expect(siteIds).toEqual([7n]);
      onSuccess?.([createBuilding(1n, "Building A")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(
      <BuildingSelectionModal
        open
        selectedBuildingIds={["1", "99"]}
        scope={{ siteIds: [7n], includeUnassigned: false }}
        onDismiss={vi.fn()}
        onSave={onSave}
      />,
    );

    await waitFor(() => expect(screen.getByText("Building A")).toBeVisible());
    expect(screen.queryByText("Building B")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Done" }));
    // Building 99 is off-site (not in the scoped list) but was already
    // selected, so it's preserved rather than silently dropped on save.
    expect(onSave).toHaveBeenCalledWith(["1", "99"]);
  });
});
