import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";
import FrigateUpdateDialog from "./FrigateUpdateDialog";
import {
  FrigateInstallationType,
  FrigatePackageSchema,
  type GetSupportResponse,
  GetSupportResponseSchema,
  InstanceSupportSchema,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { STATUSES } from "@/shared/features/toaster";

const mocks = vi.hoisted(() => ({
  listPackages: vi.fn(),
  getSupport: vi.fn(),
  startUpdate: vi.fn(),
  pushToast: vi.fn(),
}));

vi.mock("@/protoFleet/api/useFrigatePackages", () => ({
  useFrigatePackages: () => ({ listPackages: mocks.listPackages }),
}));
vi.mock("@/protoFleet/api/useFrigateUpdates", () => ({
  useFrigateUpdateActions: () => ({ getSupport: mocks.getSupport, startUpdate: mocks.startUpdate }),
}));
vi.mock("@/shared/features/toaster", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/features/toaster")>()),
  pushToast: mocks.pushToast,
}));

const PACKAGE_A = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const PACKAGE_B = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

const KEY_ID = `sha256:${"cd".repeat(32)}`;
const UNCHECKED = /Fleet did not verify this package's signature/;

const pkg = (id: string, version: string, releaseKeyId = KEY_ID) =>
  create(FrigatePackageSchema, {
    id,
    version,
    architecture: "arm64",
    filename: `frigate_${version}_arm64.deb`,
    releaseKeyId,
    storedOnThisHost: true,
  });

const readyInstance = (instanceId: string, overrides: Record<string, unknown> = {}) =>
  create(InstanceSupportSchema, {
    instanceId,
    runningVersion: "1.3.0",
    architecture: "arm64",
    installationType: FrigateInstallationType.DEBIAN_PACKAGE,
    helperAvailable: true,
    selfCoordinationSupported: true,
    remoteTriggerAllowed: true,
    etcdMembersTotal: 3,
    etcdMembersHealthy: 3,
    ...overrides,
  });

const eligible = (): GetSupportResponse =>
  create(GetSupportResponseSchema, {
    eligible: true,
    instances: [readyInstance("frigate-a"), readyInstance("frigate-b")],
  });

const container = { deviceIdentifier: "ct1/board", name: "Container 1" };

const renderDialog = () => {
  const onClose = vi.fn();
  const onStarted = vi.fn();
  render(<FrigateUpdateDialog container={container} onClose={onClose} onStarted={onStarted} />);
  return { onClose, onStarted };
};

const choosePackage = async (user: ReturnType<typeof userEvent.setup>, version: string) => {
  await user.click(await screen.findByRole("button", { name: "Package" }));
  await user.click(await screen.findByRole("option", { name: new RegExp(version) }));
};

const startButton = () => screen.getByRole("button", { name: "Start update" });

beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe = vi.fn();
      disconnect = vi.fn();
    },
  );
  mocks.listPackages.mockResolvedValue([pkg(PACKAGE_A, "1.4.0"), pkg(PACKAGE_B, "1.3.5")]);
  mocks.getSupport.mockImplementation(async () => eligible());
  mocks.startUpdate.mockResolvedValue({ jobId: "job-1" });
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("FrigateUpdateDialog", () => {
  it("states plainly that both controllers stop mining control briefly and together", async () => {
    renderDialog();
    expect(screen.getByTestId("frigate-update-disruption")).toHaveTextContent(
      "Both controllers stop mining control together, briefly.",
    );
  });

  it("shows each controller's readiness", async () => {
    renderDialog();

    const controllers = await screen.findByRole("list", { name: "Controllers" });
    expect(within(controllers).getAllByRole("listitem")).toHaveLength(2);
    expect(within(controllers).getByText("frigate-a")).toBeInTheDocument();
    expect(within(controllers).getAllByText("Version 1.3.0, arm64")).toHaveLength(2);
    expect(within(controllers).getAllByText("Ready")).toHaveLength(2);
  });

  it("explains a disabled remote trigger as an action, with the code, and keeps starting disabled", async () => {
    mocks.getSupport.mockResolvedValue(
      create(GetSupportResponseSchema, {
        eligible: false,
        ineligibleCode: "UPDATE_REMOTE_TRIGGER_DISABLED",
        ineligibleReason: "remote trigger is disabled",
        instances: [readyInstance("frigate-a"), readyInstance("frigate-b", { remoteTriggerAllowed: false })],
      }),
    );
    const user = userEvent.setup();
    renderDialog();
    await choosePackage(user, "1.4.0");

    const callout = await screen.findByTestId("frigate-update-ineligible");
    expect(callout).toHaveTextContent("set update.allow_remote_trigger to true");
    expect(callout).toHaveTextContent("Code: UPDATE_REMOTE_TRIGGER_DISABLED");
    const rows = within(screen.getByRole("list", { name: "Controllers" })).getAllByRole("listitem");
    expect(rows[1]).toHaveTextContent("update.allow_remote_trigger");
    expect(rows[0]).toHaveTextContent("Ready");
    expect(startButton()).toBeDisabled();
  });

  it("explains an unsupported helper and an unknown code with the server's reason", async () => {
    mocks.getSupport.mockResolvedValue(
      create(GetSupportResponseSchema, {
        eligible: false,
        ineligibleCode: "UPDATE_HELPER_UNAVAILABLE",
        instances: [readyInstance("frigate-a", { helperAvailable: false }), readyInstance("frigate-b")],
      }),
    );
    renderDialog();
    expect(await screen.findByTestId("frigate-update-ineligible")).toHaveTextContent("update helper is not available");

    mocks.getSupport.mockResolvedValue(
      create(GetSupportResponseSchema, {
        eligible: false,
        ineligibleCode: "FLEET_NEW_REASON",
        ineligibleReason: "The pair is in a state Fleet doesn't know.",
        instances: [],
      }),
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Check again" }));
    const callout = await screen.findByText("The pair is in a state Fleet doesn't know.");
    expect(callout).toBeInTheDocument();
    expect(screen.getByText("Code: FLEET_NEW_REASON")).toBeInTheDocument();
  });

  it("keeps the server's detail alongside the text for a known code", async () => {
    mocks.getSupport.mockResolvedValue(
      create(GetSupportResponseSchema, {
        eligible: false,
        ineligibleCode: "FLEET_INSUFFICIENT_SPACE",
        ineligibleReason: "controller frigate-b has 100 bytes free for staging; the package needs 200",
        instances: [readyInstance("frigate-a"), readyInstance("frigate-b")],
      }),
    );
    renderDialog();
    const callout = await screen.findByTestId("frigate-update-ineligible");
    expect(callout).toHaveTextContent("A controller doesn't have enough free disk space for the package.");
    expect(callout).toHaveTextContent("controller frigate-b has 100 bytes free for staging; the package needs 200");
    expect(callout).toHaveTextContent("Code: FLEET_INSUFFICIENT_SPACE");
  });

  it("falls back to a generic message when the server gives no reason", async () => {
    mocks.getSupport.mockResolvedValue(create(GetSupportResponseSchema, { eligible: false, instances: [] }));
    renderDialog();
    expect(await screen.findByTestId("frigate-update-ineligible")).toHaveTextContent(
      "This container can't be updated right now.",
    );
  });

  it("shows a support check failure with a way to try again", async () => {
    // The reason Fleet gives for a container behind a Fleet Node that is too old.
    const reason =
      "The Fleet Node that manages this container does not support controller software updates. Upgrade the Fleet Node, then try again.";
    mocks.getSupport.mockRejectedValueOnce(new ConnectError(reason, Code.FailedPrecondition));
    const user = userEvent.setup();
    renderDialog();

    expect(await screen.findByTestId("frigate-update-support-error")).toHaveTextContent(reason);
    await user.click(screen.getByRole("button", { name: "Check again" }));
    expect(await screen.findByRole("list", { name: "Controllers" })).toBeInTheDocument();
  });

  it("reads support again for the chosen package", async () => {
    const user = userEvent.setup();
    renderDialog();
    await screen.findByRole("list", { name: "Controllers" });
    expect(mocks.getSupport.mock.calls[0].slice(0, 2)).toEqual(["ct1/board", undefined]);

    await choosePackage(user, "1.4.0");

    await waitFor(() => expect(mocks.getSupport.mock.lastCall?.slice(0, 2)).toEqual(["ct1/board", PACKAGE_A]));
  });

  it("points to Settings when there are no packages", async () => {
    mocks.listPackages.mockResolvedValue([]);
    renderDialog();
    expect(await screen.findByText(/No controller software is uploaded/)).toBeInTheDocument();
    expect(startButton()).toBeDisabled();
  });

  it("offers only packages stored on this Fleet host and says why others are missing", async () => {
    mocks.listPackages.mockResolvedValue([
      create(FrigatePackageSchema, { id: PACKAGE_B, version: "1.5.0", storedOnThisHost: false }),
      pkg(PACKAGE_A, "1.4.0"),
    ]);
    renderDialog();
    expect(await screen.findByText(/1 package is stored on another Fleet host/)).toBeInTheDocument();
    // The one usable package is preselected; the other is never offered.
    await waitFor(() => expect(mocks.getSupport.mock.lastCall?.[1]).toBe(PACKAGE_A));
    expect(screen.queryByText(/1\.5\.0/)).not.toBeInTheDocument();
  });

  it("does not claim nothing was uploaded when every package is on another host", async () => {
    mocks.listPackages.mockResolvedValue([create(FrigatePackageSchema, { id: PACKAGE_B, version: "1.5.0" })]);
    renderDialog();
    expect(await screen.findByText(/stored on another Fleet host/)).toBeInTheDocument();
    expect(screen.queryByText(/No controller software is uploaded/)).not.toBeInTheDocument();
    expect(startButton()).toBeDisabled();
  });

  it("preselects the only package", async () => {
    mocks.listPackages.mockResolvedValue([pkg(PACKAGE_A, "1.4.0")]);
    renderDialog();
    await waitFor(() => expect(mocks.getSupport.mock.lastCall?.[1]).toBe(PACKAGE_A));
    await waitFor(() => expect(startButton()).toBeEnabled());
  });

  it("says when Fleet did not verify the chosen package's signature, and still lets it start", async () => {
    mocks.listPackages.mockResolvedValue([pkg(PACKAGE_A, "1.4.0", ""), pkg(PACKAGE_B, "1.3.5")]);
    const user = userEvent.setup();
    renderDialog();
    await screen.findByRole("list", { name: "Controllers" });
    expect(screen.queryByText(UNCHECKED)).not.toBeInTheDocument();

    await choosePackage(user, "1.4.0");
    expect(await screen.findByText(UNCHECKED)).toBeInTheDocument();
    await waitFor(() => expect(startButton()).toBeEnabled());

    await choosePackage(user, "1.3.5");
    await waitFor(() => expect(screen.queryByText(UNCHECKED)).not.toBeInTheDocument());
  });

  it("will not start without a package", async () => {
    renderDialog();
    await screen.findByRole("list", { name: "Controllers" });
    expect(startButton()).toBeDisabled();
  });

  it("starts the update with an idempotency key, reports it, and closes", async () => {
    const user = userEvent.setup();
    const { onClose, onStarted } = renderDialog();
    await choosePackage(user, "1.4.0");
    await waitFor(() => expect(startButton()).toBeEnabled());

    await user.click(startButton());

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(mocks.startUpdate).toHaveBeenCalledWith({
      deviceIdentifier: "ct1/board",
      packageId: PACKAGE_A,
      idempotencyKey: expect.stringMatching(/^[0-9a-f-]{36}$/),
    });
    expect(onStarted).toHaveBeenCalledWith({ jobId: "job-1" });
    expect(mocks.pushToast).toHaveBeenCalledWith({
      message: "Container 1: controller software update started.",
      status: STATUSES.success,
    });
  });

  it("reuses the idempotency key when the same update is resubmitted after a failure", async () => {
    mocks.startUpdate
      .mockRejectedValueOnce(new ConnectError("deadline exceeded", Code.DeadlineExceeded))
      .mockResolvedValueOnce({ jobId: "job-1" });
    const user = userEvent.setup();
    const { onClose } = renderDialog();
    await choosePackage(user, "1.4.0");
    await waitFor(() => expect(startButton()).toBeEnabled());

    await user.click(startButton());
    expect(await screen.findByRole("alert")).toHaveTextContent("deadline exceeded");
    expect(onClose).not.toHaveBeenCalled();
    await user.click(startButton());

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const [first, second] = mocks.startUpdate.mock.calls;
    expect(second[0].idempotencyKey).toBe(first[0].idempotencyKey);
  });

  it("uses a new key when the operator picks a different package", async () => {
    mocks.startUpdate.mockRejectedValue(new ConnectError("unavailable", Code.Unavailable));
    const user = userEvent.setup();
    renderDialog();
    await choosePackage(user, "1.4.0");
    await waitFor(() => expect(startButton()).toBeEnabled());
    await user.click(startButton());
    await screen.findByRole("alert");

    await choosePackage(user, "1.3.5");
    await waitFor(() => expect(startButton()).toBeEnabled());
    await user.click(startButton());
    await waitFor(() => expect(mocks.startUpdate).toHaveBeenCalledTimes(2));

    const [first, second] = mocks.startUpdate.mock.calls;
    expect(second[0].packageId).toBe(PACKAGE_B);
    expect(second[0].idempotencyKey).not.toBe(first[0].idempotencyKey);
  });
});
