import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ManageSecurityModal, { type MinerGroup } from "./ManageSecurityModal";

vi.mock("@/shared/assets/icons", () => ({
  DismissCircleDark: vi.fn(({ onClick }) => <button data-testid="dismiss-icon" onClick={onClick} />),
  Fleet: vi.fn(() => <div data-testid="fleet-icon" />),
  LogoAlt: vi.fn(() => <div data-testid="logoalt-icon" />),
  Success: vi.fn(() => <div data-testid="success-icon" />),
}));

vi.mock("@/shared/assets/icons/constants", () => ({
  iconSizes: { medium: "w-5 h-5", small: "w-4 h-4" },
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerList/constants", () => ({
  minerTypes: { protoRig: "proto", bitmain: "bitmain" },
}));

vi.mock("@/shared/components/Button", () => ({
  default: vi.fn(({ children, onClick, disabled, loading }) => (
    <button onClick={onClick} disabled={disabled} data-testid="action-button" data-loading={String(loading)}>
      {children}
    </button>
  )),
  sizes: { base: "base" },
  variants: { primary: "primary", secondary: "secondary" },
}));

vi.mock("@/shared/components/Divider", () => ({
  default: vi.fn(() => <hr data-testid="divider" />),
}));

vi.mock("@/shared/components/Header", () => ({
  default: vi.fn(({ title, icon, buttons }) => (
    <div data-testid="header">
      <span>{title}</span>
      <div data-testid="header-icon">{icon}</div>
      {buttons?.map((b: { text: string; onClick: () => void }, i: number) => (
        <button key={i} onClick={b.onClick} data-testid={`header-button-${i}`}>
          {b.text}
        </button>
      ))}
    </div>
  )),
}));

vi.mock("@/shared/components/PageOverlay", () => ({
  default: vi.fn(({ open, children }) => (open ? <div data-testid="page-overlay">{children}</div> : null)),
}));

vi.mock("@/shared/components/Row", () => ({
  default: vi.fn(({ children, prefixIcon, suffixIcon }) => (
    <div data-testid="row">
      <div data-testid="row-prefix">{prefixIcon}</div>
      <div data-testid="row-content">{children}</div>
      <div data-testid="row-suffix">{suffixIcon}</div>
    </div>
  )),
}));

const makeGroup = (overrides: Partial<MinerGroup>): MinerGroup => ({
  name: "Proto Rig",
  model: "Proto Rig",
  manufacturer: "proto",
  count: 1,
  deviceIdentifiers: ["device-1"],
  status: "pending",
  ...overrides,
});

describe("ManageSecurityModal", () => {
  const mockOnUpdateGroup = vi.fn();
  const mockOnDismiss = vi.fn();
  const mockOnDone = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("Visibility", () => {
    it("renders when open is true", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({})]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("page-overlay")).toBeInTheDocument();
    });

    it("does not render when open is false", () => {
      render(
        <ManageSecurityModal
          open={false}
          minerGroups={[makeGroup({})]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.queryByTestId("page-overlay")).not.toBeInTheDocument();
    });
  });

  describe("Group sorting", () => {
    it("places proto rigs before non-proto groups", () => {
      const groups = [
        makeGroup({ name: "Antminer S19", manufacturer: "bitmain", model: "S19" }),
        makeGroup({ name: "Proto Rig", manufacturer: "proto", model: "Proto Rig" }),
      ];
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={groups}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      const rows = screen.getAllByTestId("row-content");
      expect(rows[0]).toHaveTextContent("Proto Rig");
      expect(rows[1]).toHaveTextContent("Antminer S19");
    });

    it("sorts non-proto groups alphabetically by model", () => {
      const groups = [
        makeGroup({ name: "Bitmain S21", manufacturer: "bitmain", model: "S21" }),
        makeGroup({ name: "Bitmain S17", manufacturer: "bitmain", model: "S17" }),
        makeGroup({ name: "Bitmain S19", manufacturer: "bitmain", model: "S19" }),
      ];
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={groups}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      const rows = screen.getAllByTestId("row-content");
      expect(rows[0]).toHaveTextContent("Bitmain S17");
      expect(rows[1]).toHaveTextContent("Bitmain S19");
      expect(rows[2]).toHaveTextContent("Bitmain S21");
    });
  });

  describe("Icons", () => {
    it("shows LogoAlt icon for proto rig with pending status", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ manufacturer: "proto", status: "pending" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("logoalt-icon")).toBeInTheDocument();
    });

    it("shows Fleet icon for non-proto miner with pending status", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ manufacturer: "bitmain", status: "pending" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("fleet-icon")).toBeInTheDocument();
    });

    it("shows Success icon when status is updated, regardless of manufacturer", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ manufacturer: "bitmain", status: "updated" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("success-icon")).toBeInTheDocument();
      expect(screen.queryByTestId("fleet-icon")).not.toBeInTheDocument();
    });

    it("shows Success icon for proto rig when status is updated", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ manufacturer: "proto", status: "updated" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("success-icon")).toBeInTheDocument();
      expect(screen.queryByTestId("logoalt-icon")).not.toBeInTheDocument();
    });
  });

  describe("Action buttons", () => {
    it("shows Update button for pending status", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ status: "pending" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("action-button")).toHaveTextContent("Update");
    });

    it("shows Update button for failed status", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ status: "failed" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("action-button")).toHaveTextContent("Update");
    });

    it("shows Update button in loading state for loading status", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ status: "loading" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByTestId("action-button")).toHaveAttribute("data-loading", "true");
      expect(screen.getByTestId("action-button")).toHaveTextContent("Update");
    });

    it("shows no action button for updated status", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ status: "updated" })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.queryByTestId("action-button")).not.toBeInTheDocument();
    });
  });

  describe("Event handlers", () => {
    it("calls onUpdateGroup with the group when Update is clicked", () => {
      const group = makeGroup({ status: "pending", name: "Antminer S19", manufacturer: "bitmain" });
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[group]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      fireEvent.click(screen.getByTestId("action-button"));
      expect(mockOnUpdateGroup).toHaveBeenCalledWith(group);
    });

    it("calls onDone when the Done header button is clicked", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({})]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      fireEvent.click(screen.getByTestId("header-button-0"));
      expect(mockOnDone).toHaveBeenCalled();
    });

    it("calls onDismiss when the dismiss icon is clicked", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({})]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      fireEvent.click(screen.getByTestId("dismiss-icon"));
      expect(mockOnDismiss).toHaveBeenCalled();
    });
  });

  describe("Miner count display", () => {
    it("shows plural 'miners' for count greater than 1", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ count: 5 })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByText("5 miners")).toBeInTheDocument();
    });

    it("shows singular 'miner' for count of 1", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[makeGroup({ count: 1 })]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      expect(screen.getByText("1 miner")).toBeInTheDocument();
    });
  });

  describe("Dividers", () => {
    it("renders dividers between groups but not after the last one", () => {
      render(
        <ManageSecurityModal
          open={true}
          minerGroups={[
            makeGroup({ model: "Proto Rig", manufacturer: "proto" }),
            makeGroup({ name: "Antminer S19", model: "S19", manufacturer: "bitmain" }),
            makeGroup({ name: "Antminer S21", model: "S21", manufacturer: "bitmain" }),
          ]}
          onUpdateGroup={mockOnUpdateGroup}
          onDismiss={mockOnDismiss}
          onDone={mockOnDone}
        />,
      );
      // 3 groups → 2 dividers (between groups, not after last)
      expect(screen.getAllByTestId("divider")).toHaveLength(2);
    });
  });
});
