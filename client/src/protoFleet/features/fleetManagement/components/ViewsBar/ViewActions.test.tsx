import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import userEvent from "@testing-library/user-event";
import ViewActions from "./ViewActions";
import { getSavedViewsStorageKey, VIEW_URL_PARAM } from "@/protoFleet/features/fleetManagement/views/savedViews";
import useMinerViews from "@/protoFleet/features/fleetManagement/views/useMinerViews";

const STORAGE_KEY = getSavedViewsStorageKey("alice");

const LocationProbe = ({ onLocation }: { onLocation: (search: string) => void }) => {
  const location = useLocation();
  onLocation(location.search);
  return null;
};

const ViewActionsHarness = ({ onLocation }: { onLocation: (search: string) => void }) => {
  const viewsState = useMinerViews("alice");
  return (
    <>
      <ViewActions viewsState={viewsState} availableGroups={[]} availableRacks={[]} />
      <LocationProbe onLocation={onLocation} />
    </>
  );
};

const renderViewActions = (initialEntries: string[] = ["/"]) => {
  const locations: string[] = [];
  const captureLocation = (search: string) => {
    locations.push(search);
  };

  const utils = render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route path="/" element={<ViewActionsHarness onLocation={captureLocation} />} />
      </Routes>
    </MemoryRouter>,
  );

  return {
    ...utils,
    currentSearch: () => locations[locations.length - 1] ?? "",
  };
};

const readPersistedRecord = () => {
  const raw = localStorage.getItem(STORAGE_KEY);
  return raw ? JSON.parse(raw) : null;
};

const seedRecord = (record: object) => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(record));
};

describe("ViewActions", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("renders nothing when no view is active", () => {
    renderViewActions();
    expect(screen.queryByTestId("view-actions-reset-button")).not.toBeInTheDocument();
  });

  it("renders nothing when the active view is unmodified", () => {
    renderViewActions(["/?view=needs-attention&status=needs-attention"]);
    expect(screen.queryByTestId("view-actions-reset-button")).not.toBeInTheDocument();
  });

  it("renders only Reset when a built-in view is dirty", () => {
    renderViewActions(["/?view=needs-attention&status=needs-attention&model=S21"]);
    expect(screen.getByTestId("view-actions-reset-button")).toBeInTheDocument();
    expect(screen.queryByTestId("view-actions-update-button")).not.toBeInTheDocument();
  });

  it("renders Reset and Update view when a user view is dirty", () => {
    seedRecord({
      version: 1,
      views: [{ id: "u1", name: "Mine", searchParams: "status=offline", createdAt: "2026-04-30T00:00:00.000Z" }],
      deletedBuiltInIds: [],
    });
    renderViewActions(["/?view=u1&status=offline&model=S21"]);
    expect(screen.getByTestId("view-actions-reset-button")).toBeInTheDocument();
    expect(screen.getByTestId("view-actions-update-button")).toBeInTheDocument();
  });

  it("Reset restores the saved view's params", async () => {
    const user = userEvent.setup();
    const { currentSearch } = renderViewActions(["/?view=needs-attention&status=needs-attention&model=S21"]);

    await user.click(screen.getByTestId("view-actions-reset-button"));

    await waitFor(() => {
      const params = new URLSearchParams(currentSearch());
      expect(params.get("model")).toBeNull();
      expect(params.get("status")).toBe("needs-attention");
      expect(params.get(VIEW_URL_PARAM)).toBe("needs-attention");
    });
  });

  it("Update view writes current params back to the saved view", async () => {
    const user = userEvent.setup();

    seedRecord({
      version: 1,
      views: [{ id: "u1", name: "Mine", searchParams: "status=offline", createdAt: "2026-04-30T00:00:00.000Z" }],
      deletedBuiltInIds: [],
    });

    renderViewActions(["/?view=u1&status=offline&model=S21"]);

    await user.click(screen.getByTestId("view-actions-update-button"));
    await user.click(screen.getByText("Update"));

    await waitFor(() => {
      const stored = readPersistedRecord();
      expect(stored.views[0].searchParams).toBe("model=S21&status=offline");
    });
  });

  it("syncs URL to saved params after updating with Include sort order off (view becomes clean)", async () => {
    const user = userEvent.setup();

    // Saved view has sort baked in; current URL adds an extra filter so the
    // view is dirty and the Update button is visible.
    seedRecord({
      version: 1,
      views: [
        {
          id: "u1",
          name: "Mine",
          searchParams: "dir=desc&sort=hashrate&status=offline",
          createdAt: "2026-04-30T00:00:00.000Z",
        },
      ],
      deletedBuiltInIds: [],
    });

    const { currentSearch } = renderViewActions(["/?view=u1&status=offline&sort=hashrate&dir=desc&model=S21"]);

    await user.click(screen.getByTestId("view-actions-update-button"));
    // Toggle "Include sort order" off — the only checkbox in the modal.
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByText("Update"));

    // Saved view should drop sort/dir but keep the new model filter.
    await waitFor(() => {
      const stored = readPersistedRecord();
      expect(stored.views[0].searchParams).toBe("model=S21&status=offline");
    });

    // URL should match the saved view (no sort/dir, model still there).
    await waitFor(() => {
      const params = new URLSearchParams(currentSearch());
      expect(params.get("sort")).toBeNull();
      expect(params.get("dir")).toBeNull();
      expect(params.get("status")).toBe("offline");
      expect(params.get("model")).toBe("S21");
      expect(params.get(VIEW_URL_PARAM)).toBe("u1");
    });

    // Reset/Update view actions disappear because state is clean again.
    await waitFor(() => {
      expect(screen.queryByTestId("view-actions-reset-button")).not.toBeInTheDocument();
    });
  });
});
