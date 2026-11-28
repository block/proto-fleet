import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FrigatePackageUploadDialog from "./FrigatePackageUploadDialog";

const mocks = vi.hoisted(() => ({ uploadPackage: vi.fn() }));

vi.mock("@/protoFleet/api/useFrigatePackages", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFrigatePackages")>()),
  useFrigatePackages: () => ({ uploadPackage: mocks.uploadPackage }),
}));

// The names are those of a published release: the statement and signature names contain ".deb" too.
const deb = new File(["deb"], "frigate_1.4.0_linux_arm64.deb");
const statement = new File(["{}"], "frigate_1.4.0_linux_arm64.deb.statement.json");
const signature = new File([new Uint8Array([0x30, 0x80])], "frigate_1.4.0_linux_arm64.deb.statement.sig");
const publicKey = new File(["-----BEGIN PUBLIC KEY-----"], "frigate-release-2026.pem");

const picker = () => screen.getByLabelText("Package files");
const choose = (...files: File[]) => fireEvent.change(picker(), { target: { files } });
const upload = () => screen.getByRole("button", { name: /^(Upload|Try again)$/ });
const hint = () => screen.getByRole("status");

const renderDialog = () => {
  const onSuccess = vi.fn();
  const onDismiss = vi.fn();
  render(<FrigatePackageUploadDialog open onSuccess={onSuccess} onDismiss={onDismiss} />);
  return { onSuccess, onDismiss };
};

beforeEach(() => vi.clearAllMocks());

describe("FrigatePackageUploadDialog file selection", () => {
  it("offers one picker for several files, filtered to the release file types", () => {
    renderDialog();

    expect(picker()).toHaveAttribute("multiple");
    expect(picker()).toHaveAttribute("accept", ".deb,.json,.sig,.pem");
    expect(screen.getAllByRole("button", { name: "Choose files" }).length).toBeGreaterThan(0);
    expect(hint()).toHaveTextContent("Select the .deb, release statement and signature. Public key is optional.");
    expect(upload()).toBeDisabled();
  });

  it("labels each file by its type when all are selected in one go", () => {
    renderDialog();

    choose(signature, publicKey, deb, statement);

    // The labels follow a fixed order, whatever order the files were selected in.
    const labels = ["Debian package (.deb)", "Release statement", "Release signature", "Public key"];
    const shown = labels.map((label) => screen.getByText(label));
    for (let index = 1; index < shown.length; index++) {
      expect(shown[index - 1].compareDocumentPosition(shown[index]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    for (const file of [deb, statement, signature, publicKey]) {
      expect(screen.getByText(file.name)).toBeInTheDocument();
    }
    expect(hint()).toHaveTextContent("All required files selected.");
    expect(upload()).toBeEnabled();
  });

  it("does not need the public key", () => {
    renderDialog();

    choose(deb, statement, signature);

    expect(screen.queryByText("Public key")).not.toBeInTheDocument();
    expect(hint()).toHaveTextContent("All required files selected.");
    expect(upload()).toBeEnabled();
  });

  it("says which required files are still needed", () => {
    renderDialog();

    choose(deb);
    expect(hint()).toHaveTextContent("Still needed: release statement, release signature.");
    expect(upload()).toBeDisabled();

    // A later selection adds to the chosen files.
    choose(signature);
    expect(hint()).toHaveTextContent("Still needed: release statement.");
    expect(screen.getByText(deb.name)).toBeInTheDocument();

    choose(statement);
    expect(upload()).toBeEnabled();
  });

  it("replaces a file when another of the same type is selected", () => {
    renderDialog();
    choose(deb, statement, signature);
    const newer = new File(["newer"], "frigate_1.4.1_linux_arm64.deb");

    choose(newer);

    expect(screen.getByText(newer.name)).toBeInTheDocument();
    expect(screen.queryByText(deb.name)).not.toBeInTheDocument();
    expect(screen.getByText(statement.name)).toBeInTheDocument();
  });

  it("lets a chosen file be removed", () => {
    renderDialog();
    choose(deb, statement, signature);

    fireEvent.click(screen.getAllByRole("button", { name: "Remove" })[2]);

    expect(screen.queryByText(signature.name)).not.toBeInTheDocument();
    expect(hint()).toHaveTextContent("Still needed: release signature.");
    expect(upload()).toBeDisabled();
  });

  it("refuses a file of an unsupported type and keeps the chosen files", () => {
    renderDialog();
    choose(deb);

    choose(statement, new File(["x"], "notes.txt"));

    expect(screen.getByRole("alert")).toHaveTextContent("notes.txt is not a supported file type.");
    expect(screen.getByText(deb.name)).toBeInTheDocument();
    expect(screen.queryByText(statement.name)).not.toBeInTheDocument();
  });

  it("shows a refused selection as a callout above the picker", () => {
    renderDialog();
    choose(deb, statement, signature, publicKey);

    choose(new File(["x"], "notes.txt"));

    const callout = screen.getByTestId("frigate-upload-error");
    expect(screen.getByRole("alert")).toContainElement(callout);
    expect(callout).toHaveTextContent("notes.txt is not a supported file type.");
    // Above the picker and the file list, so it stays in view when four files are listed.
    expect(callout.compareDocumentPosition(hint()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("refuses two files of the same type in one selection", () => {
    renderDialog();

    choose(deb, new File(["other"], "frigate_1.4.1_linux_arm64.deb"));

    expect(screen.getByRole("alert")).toHaveTextContent("Select only one package.");
    expect(screen.queryByText(deb.name)).not.toBeInTheDocument();
  });

  it("does not offer to try again for a selection that can't be used", () => {
    renderDialog();
    choose(deb, statement, signature);

    choose(new File(["x"], "notes.txt"));

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Upload" })).toBeEnabled();

    // The next usable selection clears the message.
    choose(publicKey);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("FrigatePackageUploadDialog upload", () => {
  it("uploads the files, shows progress, and reports the result", async () => {
    let finish!: (value: { package: object; reused: boolean }) => void;
    mocks.uploadPackage.mockImplementation(
      ({ onProgress }: { onProgress: (percent: number) => void }) =>
        new Promise((resolve) => {
          onProgress(45);
          finish = resolve;
        }),
    );
    const { onSuccess } = renderDialog();
    choose(deb, statement, signature, publicKey);

    fireEvent.click(upload());

    expect(await screen.findByText(/45% uploaded/)).toBeInTheDocument();
    expect(mocks.uploadPackage).toHaveBeenCalledWith(expect.objectContaining({ deb, statement, signature, publicKey }));
    expect(screen.getByRole("button", { name: "Uploading…" })).toBeDisabled();
    // The files can't be changed while they upload.
    expect(screen.queryByRole("button", { name: "Remove" })).not.toBeInTheDocument();

    const result = { package: { id: "p" }, reused: false };
    finish(result);
    await waitFor(() => expect(onSuccess).toHaveBeenCalledWith(result));
  });

  it("uploads without a public key", async () => {
    mocks.uploadPackage.mockResolvedValue({ package: { id: "p" }, reused: false });
    const { onSuccess } = renderDialog();
    choose(deb, statement, signature);

    fireEvent.click(upload());

    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
    expect(mocks.uploadPackage.mock.calls[0][0].publicKey).toBeUndefined();
  });

  it("stays open to warn when Fleet stored the package without verifying the signature", async () => {
    const result = {
      package: { id: "p" },
      reused: false,
      warning: "Fleet has no release key to verify the signature.",
    };
    mocks.uploadPackage.mockResolvedValue(result);
    const { onSuccess, onDismiss } = renderDialog();
    choose(deb, statement, signature);

    fireEvent.click(upload());

    const callout = await screen.findByTestId("frigate-upload-unverified");
    expect(callout).toHaveTextContent("Signature not verified");
    expect(callout).toHaveTextContent(result.warning);
    expect(onSuccess).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    expect(onSuccess).toHaveBeenCalledWith(result);
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("reports the stored package when the warning is closed without Done", async () => {
    const result = { package: { id: "p" }, reused: false, warning: "The signature does not match the public key." };
    mocks.uploadPackage.mockResolvedValue(result);
    const { onSuccess, onDismiss } = renderDialog();
    choose(deb, statement, signature);
    fireEvent.click(upload());
    await screen.findByTestId("frigate-upload-unverified");

    fireEvent.click(screen.getByRole("button", { name: /close/i }));

    expect(onSuccess).toHaveBeenCalledWith(result);
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("shows the server's failure, keeps the files, and lets the operator try again", async () => {
    mocks.uploadPackage
      .mockRejectedValueOnce(new Error("release signature is not trusted"))
      .mockResolvedValueOnce({ package: { id: "p" }, reused: false });
    const { onSuccess } = renderDialog();
    choose(deb, statement, signature);

    fireEvent.click(upload());

    expect(await screen.findByRole("alert")).toHaveTextContent("release signature is not trusted");
    expect(screen.getByTestId("frigate-upload-error")).toHaveTextContent("release signature is not trusted");
    expect(screen.queryByText(/% uploaded/)).not.toBeInTheDocument();
    expect(screen.getByText(deb.name)).toBeInTheDocument();
    expect(onSuccess).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
    expect(mocks.uploadPackage).toHaveBeenCalledTimes(2);
  });

  it("clears an old failure when a file changes", async () => {
    mocks.uploadPackage.mockRejectedValue(new Error("bad package"));
    renderDialog();
    choose(deb, statement, signature);
    fireEvent.click(upload());
    await screen.findByRole("alert");

    choose(new File(["other"], "frigate_1.4.1_linux_arm64.deb"));

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Upload" })).toBeEnabled();
  });

  it("names the file name rule of a ProtoOS package", () => {
    renderDialog();
    expect(screen.getByText("Upload package")).toBeInTheDocument();
    expect(
      screen.getByText("Keep the release file name of a ProtoOS package: proto-os_<tag>_all.deb."),
    ).toBeInTheDocument();
  });

  it("uploads a ProtoOS package with the same files", async () => {
    const protoOS = [
      new File(["deb"], "proto-os_v0.6.2-internal.14_all.deb"),
      new File(["{}"], "proto-os_v0.6.2-internal.14_all.deb.statement.json"),
      new File([new Uint8Array([0x30, 0x80])], "proto-os_v0.6.2-internal.14_all.deb.statement.sig"),
    ];
    const result = { package: { id: "p", kind: "proto_os" }, reused: false };
    mocks.uploadPackage.mockResolvedValue(result);
    const { onSuccess } = renderDialog();
    choose(...protoOS);

    fireEvent.click(upload());

    await waitFor(() => expect(onSuccess).toHaveBeenCalledWith(result));
    const [deb, statement, signature] = protoOS;
    expect(mocks.uploadPackage).toHaveBeenCalledWith(expect.objectContaining({ deb, statement, signature }));
  });

  it("shows the server's refusal of a renamed ProtoOS package and keeps the files", async () => {
    const refusal = 'a ProtoOS package for v0.6.2-internal.14 must be named "proto-os_v0.6.2-internal.14_all.deb"';
    mocks.uploadPackage.mockRejectedValue(new Error(refusal));
    const { onSuccess } = renderDialog();
    const renamed = new File(["deb"], "proto-os-latest.deb");
    choose(renamed, statement, signature);

    fireEvent.click(upload());

    expect(await screen.findByTestId("frigate-upload-error")).toHaveTextContent(refusal);
    expect(screen.getByText(renamed.name)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it("cancels an upload in progress when dismissed", async () => {
    let signal: AbortSignal | undefined;
    mocks.uploadPackage.mockImplementation(({ signal: given }: { signal: AbortSignal }) => {
      signal = given;
      return new Promise(() => {});
    });
    const { onDismiss } = renderDialog();
    choose(deb, statement, signature);
    fireEvent.click(upload());
    await waitFor(() => expect(signal).toBeDefined());

    fireEvent.click(screen.getByRole("button", { name: /close/i }));

    expect(signal?.aborted).toBe(true);
    expect(onDismiss).toHaveBeenCalled();
  });
});
