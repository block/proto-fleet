import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import userEvent from "@testing-library/user-event";
import ViewsBar from "./ViewsBar";
import { getSavedViewsStorageKey, VIEW_URL_PARAM } from "@/protoFleet/features/fleetManagement/views/savedViews";
import useMinerViews from "@/protoFleet/features/fleetManagement/views/useMinerViews";

const STORAGE_KEY = getSavedViewsStorageKey("alice");

const LocationProbe = ({ onLocation }: { onLocation: (search: string) => void }) => {
  const location = useLocation();
  onLocation(location.search);
  return null;
};

const ViewsBarHarness = ({ onLocation }: { onLocation: (search: string) => void }) => {
  const viewsState = useMinerViews("alice");
  return (
    <>
      <ViewsBar viewsState={viewsState} availableGroups={[]} availableRacks={[]} />
      <LocationProbe onLocation={onLocation} />
    </>
  );
};

const renderViewsBar = (initialEntries: string[] = ["/"]) => {
  const locations: string[] = [];
  const captureLocation = (search: string) => {
    locations.push(search);
  };

  const utils = render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route path="/" element={<ViewsBarHarness onLocation={captureLocation} />} />
      </Routes>
    </MemoryRouter>,
  );

  return {
    ...utils,
    /** The most recent location.search value the router has emitted. */
    currentSearch: () => locations[locations.length - 1] ?? "",
  };
};

const readPersistedRecord = () => {
  const raw = localStorage.getItem(STORAGE_KEY);
  return raw ? JSON.parse(raw) : null;
};

describe("ViewsBar", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("renders all built-in views as tabs by default", () => {
    renderViewsBar();
    expect(screen.getByTestId("views-bar-tab-all-miners")).toBeInTheDocument();
    expect(screen.getByTestId("views-bar-tab-needs-attention")).toBeInTheDocument();
    expect(screen.getByTestId("views-bar-tab-offline")).toBeInTheDocument();
  });

  it("activates a built-in view, writing the view id and its filters into the URL", async () => {
    const user = userEvent.setup();
    const { currentSearch } = renderViewsBar();

    await user.click(screen.getByTestId("views-bar-tab-needs-attention-activate"));

    await waitFor(() => {
      const params = new URLSearchParams(currentSearch());
      expect(params.get(VIEW_URL_PARAM)).toBe("needs-attention");
      expect(params.get("status")).toBe("needs-attention");
    });
  });

  it("flags the active tab as warning-toned when current URL drifts from active view", () => {
    renderViewsBar(["/?view=needs-attention&status=needs-attention&model=S21"]);
    const activeTab = screen.getByTestId("views-bar-tab-needs-attention");
    expect(activeTab).toHaveAttribute("data-active", "true");
    expect(activeTab.className).toContain("text-intent-warning-50");
  });

  it("persists a new user view via the modal and activates it", async () => {
    const user = userEvent.setup();
    const { currentSearch } = renderViewsBar(["/?status=offline&model=S21"]);

    await user.click(screen.getByTestId("views-bar-new-view-button"));

    const nameInput = screen.getByLabelText("Name");
    await user.type(nameInput, "S21 offline");
    await user.click(screen.getByText("Save"));

    await waitFor(() => {
      const stored = readPersistedRecord();
      expect(stored.views).toHaveLength(1);
      expect(stored.views[0].name).toBe("S21 offline");
      expect(stored.views[0].searchParams).toBe("model=S21&status=offline");
    });

    await waitFor(() => {
      const params = new URLSearchParams(currentSearch());
      expect(params.get(VIEW_URL_PARAM)).toBeTruthy();
    });
  });

  it("lists current filters and humanizes status values inside the modal", async () => {
    const user = userEvent.setup();
    renderViewsBar(["/?status=offline&model=S21"]);

    await user.click(screen.getByTestId("views-bar-new-view-button"));

    const statusRow = screen.getByTestId("view-summary-status");
    expect(statusRow).toHaveTextContent("Status:");
    expect(statusRow).toHaveTextContent("Offline");

    const modelRow = screen.getByTestId("view-summary-model");
    expect(modelRow).toHaveTextContent("S21");
  });

  it("shows the empty-state copy when no filters or sort are applied", async () => {
    const user = userEvent.setup();
    renderViewsBar();

    await user.click(screen.getByTestId("views-bar-new-view-button"));

    expect(screen.getByTestId("view-summary-empty")).toBeInTheDocument();
  });

  it("strips sort+dir from the saved view AND from the URL when the include-sort toggle is off", async () => {
    const user = userEvent.setup();
    const { currentSearch } = renderViewsBar(["/?status=offline&sort=hashrate&dir=desc"]);

    await user.click(screen.getByTestId("views-bar-new-view-button"));

    // Include-sort toggle row reflects the active sort.
    const includeSortRow = screen.getByTestId("view-summary-include-sort");
    expect(includeSortRow).toHaveTextContent("Hashrate (descending)");

    // Flip the include-sort switch (the only checkbox in the modal).
    await user.click(screen.getByRole("checkbox"));

    const nameInput = screen.getByLabelText("Name");
    await user.type(nameInput, "Sortless");
    await user.click(screen.getByText("Save"));

    await waitFor(() => {
      const stored = readPersistedRecord();
      expect(stored.views[0].searchParams).toBe("status=offline");
    });

    // URL should match the saved view (no sort/dir) so the new view is clean
    // immediately — otherwise the user sees Reset/Update view right after save.
    await waitFor(() => {
      const params = new URLSearchParams(currentSearch());
      expect(params.get("sort")).toBeNull();
      expect(params.get("dir")).toBeNull();
      expect(params.get("status")).toBe("offline");
      expect(params.get(VIEW_URL_PARAM)).toBeTruthy();
    });
  });

  it("rejects an empty name and surfaces an error in the modal", async () => {
    const user = userEvent.setup();
    renderViewsBar();

    await user.click(screen.getByTestId("views-bar-new-view-button"));
    await user.click(screen.getByText("Save"));

    expect(screen.getByText("Name is required")).toBeInTheDocument();
    expect(readPersistedRecord()).toBeNull();
  });

  it("rejects a name that matches a built-in view (case-insensitive)", async () => {
    const user = userEvent.setup();
    renderViewsBar();

    await user.click(screen.getByTestId("views-bar-new-view-button"));

    const nameInput = screen.getByLabelText("Name");
    await user.type(nameInput, "all miners");
    await user.click(screen.getByText("Save"));

    expect(screen.getByText("A view with this name already exists")).toBeInTheDocument();
    expect(readPersistedRecord()).toBeNull();
  });

  it("does not render dirty-state actions in the strip — those live in <ViewActions>", () => {
    renderViewsBar(["/?view=needs-attention&status=needs-attention&model=S21"]);
    expect(screen.queryByTestId("views-bar-modified-actions")).not.toBeInTheDocument();
    expect(screen.queryByTestId("views-bar-reset-button")).not.toBeInTheDocument();
    expect(screen.queryByTestId("views-bar-update-button")).not.toBeInTheDocument();
  });
});
