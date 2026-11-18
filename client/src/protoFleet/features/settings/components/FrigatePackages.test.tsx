import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import FrigatePackages from "./FrigatePackages";
import { FrigatePackageSchema } from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { STATUSES } from "@/shared/features/toaster";

const mocks = vi.hoisted(() => ({
  listPackages: vi.fn(),
  deletePackage: vi.fn(),
  pushToast: vi.fn(),
}));

vi.mock("@/protoFleet/api/useFrigatePackages", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFrigatePackages")>()),
  useFrigatePackages: () => ({
    listPackages: mocks.listPackages,
    deletePackage: mocks.deletePackage,
    uploadPackage: vi.fn(),
  }),
}));
vi.mock("./FrigatePackageUploadDialog", () => ({
  default: ({
    open,
    onSuccess,
    onDismiss,
  }: {
    open?: boolean;
    onSuccess: (result: { reused: boolean }) => void;
    onDismiss: () => void;
  }) =>
    open ? (
      <div role="dialog" aria-label="Upload controller software">
        <button onClick={() => onSuccess({ reused: false })}>Finish upload</button>
        <button onClick={() => onSuccess({ reused: true })}>Finish duplicate upload</button>
        <button onClick={onDismiss}>Dismiss upload</button>
      </div>
    ) : null,
}));
vi.mock("@/shared/features/toaster", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/features/toaster")>()),
  pushToast: mocks.pushToast,
}));

const PACKAGE_ID = "11111111-1111-4111-8111-111111111111";
const DIGEST = "ab".repeat(32);
const KEY_ID = `sha256:${"cd".repeat(32)}`;

const item = () =>
  create(FrigatePackageSchema, {
    id: PACKAGE_ID,
    filename: "frigate_1.4.0_arm64.deb",
    version: "1.4.0",
    architecture: "arm64",
    sha256: DIGEST,
    sizeBytes: 5_242_880n,
    releaseKeyId: KEY_ID,
    uploadedAt: timestampFromDate(new Date("2026-03-04T05:06:07Z")),
    storedOnThisHost: true,
  });

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listPackages.mockResolvedValue([item()]);
  mocks.deletePackage.mockResolvedValue(undefined);
});

describe("FrigatePackages list", () => {
  it("flags a package stored on another Fleet host", async () => {
    mocks.listPackages.mockResolvedValue([{ ...item(), storedOnThisHost: false }]);
    render(<FrigatePackages />);
    expect(await screen.findByText(/Stored on another Fleet host/)).toBeInTheDocument();
  });

  it("does not flag a package stored on this host", async () => {
    render(<FrigatePackages />);
    expect(await screen.findByText("1.4.0")).toBeInTheDocument();
    expect(screen.queryByText(/Stored on another Fleet host/)).not.toBeInTheDocument();
  });

  it("shows version, architecture, digest, signer key and upload time", async () => {
    render(<FrigatePackages />);

    expect(await screen.findByText("1.4.0")).toBeInTheDocument();
    expect(screen.getByText(/frigate_1\.4\.0_arm64\.deb, 5\.0 MB/)).toBeInTheDocument();
    expect(screen.getByText("arm64")).toBeInTheDocument();
    for (const title of ["Version", "Architecture", "Digest", "Signer key", "Uploaded"]) {
      expect(screen.getByText(title)).toBeInTheDocument();
    }
    // The full values stay available in the title when the display is shortened.
    expect(screen.getByTitle(DIGEST)).toHaveTextContent(`${DIGEST.slice(0, 22)}…`);
    expect(screen.getByTitle(KEY_ID)).toBeInTheDocument();
  });

  it("shows an empty state", async () => {
    mocks.listPackages.mockResolvedValue([]);
    render(<FrigatePackages />);
    expect(await screen.findByText("No controller software uploaded")).toBeInTheDocument();
  });

  it("shows a load failure", async () => {
    mocks.listPackages.mockRejectedValue(new ConnectError("denied", Code.PermissionDenied));
    render(<FrigatePackages />);
    expect(await screen.findByRole("alert")).toHaveTextContent("denied");
  });

  it("keeps the upload action with its own heading", async () => {
    render(<FrigatePackages />);
    await screen.findByText("1.4.0");
    const section = screen.getByRole("heading", { name: "Controller software" }).closest("section");
    expect(within(section!).getByRole("button", { name: "Upload controller software" })).toBeInTheDocument();
  });
});

describe("FrigatePackages upload", () => {
  it("refreshes the list and confirms after an upload", async () => {
    render(<FrigatePackages />);
    await screen.findByText("1.4.0");
    mocks.listPackages.mockClear();

    fireEvent.click(screen.getByRole("button", { name: "Upload controller software" }));
    fireEvent.click(await screen.findByRole("button", { name: "Finish upload" }));

    await waitFor(() => expect(mocks.listPackages).toHaveBeenCalledTimes(1));
    expect(mocks.pushToast).toHaveBeenCalledWith({
      message: "Controller software uploaded",
      status: STATUSES.success,
    });
    expect(screen.queryByRole("dialog", { name: "Upload controller software" })).not.toBeInTheDocument();
  });

  it("says when the package was already uploaded", async () => {
    render(<FrigatePackages />);
    await screen.findByText("1.4.0");

    fireEvent.click(screen.getByRole("button", { name: "Upload controller software" }));
    fireEvent.click(await screen.findByRole("button", { name: "Finish duplicate upload" }));

    expect(mocks.pushToast).toHaveBeenCalledWith({
      message: "That package was already uploaded",
      status: STATUSES.success,
    });
  });
});

describe("FrigatePackages delete", () => {
  const openDelete = async () => {
    render(<FrigatePackages />);
    await screen.findByText("1.4.0");
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    return screen.findByTestId("delete-frigate-package-dialog");
  };

  it("confirms before deleting and refreshes the list", async () => {
    const dialog = await openDelete();
    expect(dialog).toHaveTextContent("frigate_1.4.0_arm64.deb");
    expect(mocks.deletePackage).not.toHaveBeenCalled();
    mocks.listPackages.mockClear();

    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(mocks.deletePackage).toHaveBeenCalledWith(PACKAGE_ID));
    await waitFor(() => expect(mocks.listPackages).toHaveBeenCalledTimes(1));
    expect(mocks.pushToast).toHaveBeenCalledWith({
      message: "Deleted frigate_1.4.0_arm64.deb",
      status: STATUSES.success,
    });
  });

  it("keeps the dialog open and shows why the server refused", async () => {
    mocks.deletePackage.mockRejectedValue(
      new ConnectError("package is in use by an active update", Code.FailedPrecondition),
    );
    const dialog = await openDelete();

    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("package is in use by an active update");
    expect(screen.getByTestId("delete-frigate-package-dialog")).toBeInTheDocument();
  });

  it("deletes nothing when the dialog is cancelled", async () => {
    const dialog = await openDelete();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(mocks.deletePackage).not.toHaveBeenCalled();
  });
});
