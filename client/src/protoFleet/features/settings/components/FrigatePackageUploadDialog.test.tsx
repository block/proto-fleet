import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FrigatePackageUploadDialog from "./FrigatePackageUploadDialog";

const mocks = vi.hoisted(() => ({ uploadPackage: vi.fn() }));

vi.mock("@/protoFleet/api/useFrigatePackages", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFrigatePackages")>()),
  useFrigatePackages: () => ({ uploadPackage: mocks.uploadPackage }),
}));

const deb = new File(["deb"], "frigate_1.4.0_arm64.deb");
const statement = new File(["{}"], "statement.json");
const signature = new File([new Uint8Array([0x30, 0x80])], "statement.sig");

const choose = (label: string, file: File) =>
  fireEvent.change(screen.getByLabelText(label), { target: { files: [file] } });
const chooseAll = () => {
  choose("Package", deb);
  choose("Release statement", statement);
  choose("Release signature", signature);
};
const upload = () => screen.getByRole("button", { name: /^(Upload|Try again)$/ });

const renderDialog = () => {
  const onSuccess = vi.fn();
  const onDismiss = vi.fn();
  render(<FrigatePackageUploadDialog open onSuccess={onSuccess} onDismiss={onDismiss} />);
  return { onSuccess, onDismiss };
};

beforeEach(() => vi.clearAllMocks());

describe("FrigatePackageUploadDialog", () => {
  it("needs the package, statement and signature before it can upload", () => {
    renderDialog();
    expect(upload()).toBeDisabled();

    choose("Package", deb);
    choose("Release statement", statement);
    expect(upload()).toBeDisabled();

    choose("Release signature", signature);
    expect(upload()).toBeEnabled();
  });

  it("lets a chosen file be removed", () => {
    renderDialog();
    chooseAll();
    fireEvent.click(screen.getAllByRole("button", { name: "Remove" })[2]);
    expect(upload()).toBeDisabled();
    expect(screen.getByLabelText("Release signature")).toBeInTheDocument();
  });

  it("uploads the three files, shows progress, and reports the result", async () => {
    let finish!: (value: { package: object; reused: boolean }) => void;
    mocks.uploadPackage.mockImplementation(
      ({ onProgress }: { onProgress: (percent: number) => void }) =>
        new Promise((resolve) => {
          onProgress(45);
          finish = resolve;
        }),
    );
    const { onSuccess } = renderDialog();
    chooseAll();

    fireEvent.click(upload());

    expect(await screen.findByText(/45% uploaded/)).toBeInTheDocument();
    expect(mocks.uploadPackage).toHaveBeenCalledWith(expect.objectContaining({ deb, statement, signature }));
    expect(screen.getByRole("button", { name: "Uploading…" })).toBeDisabled();

    const result = { package: { id: "p" }, reused: false };
    finish(result);
    await waitFor(() => expect(onSuccess).toHaveBeenCalledWith(result));
  });

  it("shows the server's failure and lets the operator try again", async () => {
    mocks.uploadPackage
      .mockRejectedValueOnce(new Error("release signature is not trusted"))
      .mockResolvedValueOnce({ package: { id: "p" }, reused: false });
    const { onSuccess } = renderDialog();
    chooseAll();

    fireEvent.click(upload());

    expect(await screen.findByRole("alert")).toHaveTextContent("release signature is not trusted");
    expect(screen.queryByText(/% uploaded/)).not.toBeInTheDocument();
    expect(onSuccess).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
    expect(mocks.uploadPackage).toHaveBeenCalledTimes(2);
  });

  it("clears an old failure when a file changes", async () => {
    mocks.uploadPackage.mockRejectedValue(new Error("bad package"));
    renderDialog();
    chooseAll();
    fireEvent.click(upload());
    await screen.findByRole("alert");

    choose("Package", new File(["other"], "frigate_1.4.1_arm64.deb"));

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("cancels an upload in progress when dismissed", async () => {
    let signal: AbortSignal | undefined;
    mocks.uploadPackage.mockImplementation(({ signal: given }: { signal: AbortSignal }) => {
      signal = given;
      return new Promise(() => {});
    });
    const { onDismiss } = renderDialog();
    chooseAll();
    fireEvent.click(upload());
    await waitFor(() => expect(signal).toBeDefined());

    fireEvent.click(screen.getByRole("button", { name: /close/i }));

    expect(signal?.aborted).toBe(true);
    expect(onDismiss).toHaveBeenCalled();
  });
});
