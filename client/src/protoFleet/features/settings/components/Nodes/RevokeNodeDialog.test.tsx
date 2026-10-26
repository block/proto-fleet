import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import RevokeNodeDialog from "./RevokeNodeDialog";
import { FleetNodeHARole } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";

describe("RevokeNodeDialog", () => {
  it("groups ASIC-rs miners by manufacturer and model instead of driver", () => {
    render(
      <RevokeNodeDialog
        open
        nodeName="node-01"
        affectedDevices={[
          { manufacturer: "Bitmain", model: "S19" },
          { manufacturer: "Bitmain", model: "S19" },
          { manufacturer: "Bitmain", model: "S21" },
          { manufacturer: "MicroBT", model: "M60" },
        ]}
        isLoadingImpact={false}
        impactError=""
        onConfirm={vi.fn()}
        onDismiss={vi.fn()}
        isSubmitting={false}
      />,
    );

    expect(screen.getByText("2 paired Bitmain S19 miners will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Bitmain S21 miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired MicroBT M60 miner will lose connection.")).toBeInTheDocument();
    expect(screen.queryByText(/ASIC-RS/)).not.toBeInTheDocument();
  });

  const haProps = {
    open: true,
    nodeName: "node-01",
    affectedDevices: [{ manufacturer: "Proto", model: "Rig" }],
    isLoadingImpact: false,
    impactError: "",
    onConfirm: vi.fn(),
    onDismiss: vi.fn(),
    isSubmitting: false,
    peerName: "node-02",
  };

  it("hands devices to a ready, connected standby when revoking the active node", () => {
    // Arrange
    const peerNode = { name: "node-02", controlStreamConnected: true };

    // Act
    render(
      <RevokeNodeDialog
        {...haProps}
        haRole={FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE}
        haStandbyReady
        peerNode={peerNode}
      />,
    );

    // Assert
    expect(
      screen.getByText(/immediately loses access to Fleet.*Fleet then moves its devices to node-02/),
    ).toBeInTheDocument();
    expect(screen.getByText(/If the handoff fails, the node stays revoked.*Retry revocation/)).toBeInTheDocument();
    expect(screen.getByText(/removes the HA pair/)).toBeInTheDocument();
    expect(screen.queryByText(/will lose connection/)).not.toBeInTheDocument();
  });

  it.each([
    ["not ready", false, true, "isn't ready"],
    ["not connected", true, false, "isn't connected"],
  ])(
    "warns that devices are unpaired when the standby is %s",
    (_label, haStandbyReady, controlStreamConnected, reason) => {
      // Arrange
      const peerNode = { name: "node-02", controlStreamConnected };

      // Act
      render(
        <RevokeNodeDialog
          {...haProps}
          haRole={FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE}
          haStandbyReady={haStandbyReady}
          peerNode={peerNode}
        />,
      );

      // Assert
      expect(screen.getByText(/miner pairings and stored miner credentials are removed/)).toBeInTheDocument();
      expect(screen.getByText(new RegExp(`since node-02 ${reason} to take over`))).toBeInTheDocument();
      expect(screen.getByText("1 paired Proto Rig miner will lose connection.")).toBeInTheDocument();
    },
  );

  it("warns that devices are unpaired when the standby is missing from the node list", () => {
    // Act
    render(<RevokeNodeDialog {...haProps} haRole={FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE} haStandbyReady />);

    // Assert
    expect(screen.getByText(/since node-02 isn't connected to take over/)).toBeInTheDocument();
  });

  it("explains that the peer keeps its devices when revoking a standby", () => {
    // Arrange
    const peerNode = { name: "node-02", controlStreamConnected: true };

    // Act
    render(<RevokeNodeDialog {...haProps} haRole={FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY} peerNode={peerNode} />);

    // Assert
    expect(screen.getByText(/the HA pair is removed\. node-02 keeps its devices/)).toBeInTheDocument();
    expect(screen.queryByText(/will lose connection/)).not.toBeInTheDocument();
    expect(screen.queryByText(/No paired miners/)).not.toBeInTheDocument();
  });

  it("requires a successful impact preview before revocation", () => {
    const onConfirm = vi.fn();
    const props = {
      open: true,
      nodeName: "node-01",
      affectedDevices: [] as { manufacturer: string; model: string }[],
      isLoadingImpact: true,
      impactError: "",
      onConfirm,
      onDismiss: vi.fn(),
      isSubmitting: false,
    };
    const { rerender } = render(<RevokeNodeDialog {...props} />);
    const confirm = screen.getByRole("button", { name: "Revoke node" });

    expect(confirm).toBeDisabled();
    rerender(<RevokeNodeDialog {...props} isLoadingImpact={false} impactError="Could not load affected miners." />);
    expect(confirm).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load affected miners.");

    rerender(
      <RevokeNodeDialog
        {...props}
        isLoadingImpact={false}
        affectedDevices={[{ manufacturer: "Proto", model: "Rig" }]}
      />,
    );
    expect(screen.getByText("1 paired Proto Rig miner will lose connection.")).toBeInTheDocument();
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("keeps partial hardware details separate and handles an empty preview", () => {
    const props = {
      open: true,
      nodeName: "node-01",
      affectedDevices: [
        { manufacturer: " Bitmain ", model: " S19 " },
        { manufacturer: "Bitmain", model: "S19" },
        { manufacturer: "Other", model: "S19" },
        { manufacturer: "Bitmain", model: "" },
        { manufacturer: "", model: "S19" },
        { manufacturer: " ", model: " " },
      ],
      isLoadingImpact: false,
      impactError: "",
      onConfirm: vi.fn(),
      onDismiss: vi.fn(),
      isSubmitting: false,
    };
    const { rerender } = render(<RevokeNodeDialog {...props} />);

    expect(screen.getByText("2 paired Bitmain S19 miners will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Other S19 miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Bitmain Unknown model miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Unknown manufacturer S19 miner will lose connection.")).toBeInTheDocument();
    expect(
      screen.getByText("1 paired Unknown manufacturer Unknown model miner will lose connection."),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(5);

    rerender(<RevokeNodeDialog {...props} affectedDevices={[]} />);
    expect(screen.getByText("No paired miners will lose connection.")).toBeInTheDocument();
  });
});
