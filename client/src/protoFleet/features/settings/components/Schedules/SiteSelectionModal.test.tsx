import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import SiteSelectionModal from "./SiteSelectionModal";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const { listSitesMock, pushToastMock } = vi.hoisted(() => ({
  listSitesMock: vi.fn(),
  pushToastMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({ listSites: listSitesMock }),
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

const createSite = (id: bigint, name: string) =>
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id, name }) });

type Callbacks = { onSuccess?: (rows: ReturnType<typeof createSite>[]) => void; onFinally?: () => void };

describe("SiteSelectionModal", () => {
  beforeEach(() => {
    listSitesMock.mockReset();
    pushToastMock.mockReset();
  });

  it("lists every org site and saves the selection", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven"), createSite(9n, "Site Nine")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<SiteSelectionModal open selectedSiteIds={["7"]} onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Site Seven")).toBeVisible());
    expect(screen.getByText("Site Nine")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7"], allSelected: false });
  });

  it("prunes selected ids that are no longer present", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<SiteSelectionModal open selectedSiteIds={["7", "deleted"]} onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Site Seven")).toBeVisible());
    await user.click(screen.getByRole("button", { name: "Done" }));
    // Coverage alone is not intent: the pruned fixed list stays fixed even
    // though it happens to span every remaining site.
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7"], allSelected: false });
  });

  it("keeps a fixed list fixed even when it covers every site", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven"), createSite(9n, "Site Nine")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<SiteSelectionModal open selectedSiteIds={["7", "9"]} onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Site Seven")).toBeVisible());
    await user.click(screen.getByRole("button", { name: "Done" }));
    // An untouched open-and-Done must not widen an explicit list into a live
    // all-sites scope that would absorb future sites.
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7", "9"], allSelected: false });
  });

  it("saves a live all-sites selection only after an explicit All sites gesture", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven"), createSite(9n, "Site Nine")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<SiteSelectionModal open selectedSiteIds={[]} onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Site Seven")).toBeVisible());
    await user.click(screen.getByText("All sites"));
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7", "9"], allSelected: true });
  });

  it("preserves an all-sites seed across an untouched open-and-Done", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven"), createSite(9n, "Site Nine")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<SiteSelectionModal open selectedSiteIds={[]} allSitesSelected onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Site Seven")).toBeVisible());
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7", "9"], allSelected: true });
  });

  it("demotes an all-sites seed to a fixed list when a site is unchecked", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven"), createSite(9n, "Site Nine")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<SiteSelectionModal open selectedSiteIds={[]} allSitesSelected onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Site Seven")).toBeVisible());
    await user.click(screen.getByText("Site Nine"));
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7"], allSelected: false });
  });

  it("narrows the list to the active site and preserves off-site selections", async () => {
    // listSites returns the org's sites; the modal filters client-side to scope.
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven"), createSite(9n, "Site Nine")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(
      <SiteSelectionModal
        open
        selectedSiteIds={["7", "9"]}
        scope={{ siteIds: [7n], includeUnassigned: false }}
        onDismiss={vi.fn()}
        onSave={onSave}
      />,
    );

    await waitFor(() => expect(screen.getByText("Site Seven")).toBeVisible());
    expect(screen.queryByText("Site Nine")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Done" }));
    // Site 9 is out of scope (not offered) but was already selected, so it's
    // preserved rather than silently dropped on save.
    // Scope-narrowed lists never report allSelected: "all visible" is not all org sites.
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7", "9"], allSelected: false });
  });

  it("offers no sites under the Unassigned scope and preserves preselected ids", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([createSite(7n, "Site Seven"), createSite(9n, "Site Nine")]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(
      <SiteSelectionModal
        open
        selectedSiteIds={["7"]}
        scope={{ siteIds: [], includeUnassigned: true }}
        onDismiss={vi.fn()}
        onSave={onSave}
      />,
    );

    // A site target is incompatible with "no site" — no sites are selectable.
    await waitFor(() => expect(screen.getByText("Sites unavailable")).toBeInTheDocument());
    expect(screen.queryByText("Site Seven")).not.toBeInTheDocument();
    expect(screen.queryByText("Site Nine")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Done" }));
    // The preselected site target survives (not silently dropped).
    expect(onSave).toHaveBeenCalledWith({ siteIds: ["7"], allSelected: false });
  });

  it("preserves an all-sites seed when no sites currently exist", async () => {
    listSitesMock.mockImplementation(({ onSuccess, onFinally }: Callbacks) => {
      onSuccess?.([]);
      onFinally?.();
    });
    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<SiteSelectionModal open selectedSiteIds={[]} allSitesSelected onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("No sites configured")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Done" }));
    // Nothing was actionable, so the live "all sites" selection must survive
    // instead of silently widening the caller's scope to "everything".
    expect(onSave).toHaveBeenCalledWith({ siteIds: [], allSelected: true });
  });
});
