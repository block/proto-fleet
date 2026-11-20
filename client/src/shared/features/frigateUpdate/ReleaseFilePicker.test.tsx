import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ReleaseFilePicker from "./ReleaseFilePicker";
import {
  missingReleaseFiles,
  RELEASE_PACKAGE_FILE_KINDS,
  type ReleaseFileKind,
  type ReleaseFiles,
  sortReleaseFiles,
} from "./releaseFiles";

type Key = "deb" | "statement" | "signature" | "publicKey";
const kinds: ReleaseFileKind<Key>[] = [
  ...RELEASE_PACKAGE_FILE_KINDS,
  { key: "publicKey", extension: ".pem", label: "Public key", name: "public key", required: false },
];

// The names are those of a published release: the statement and signature names contain ".deb" too.
const deb = new File(["deb"], "frigate_1.4.0_linux_arm64.deb");
const statement = new File(["{}"], "frigate_1.4.0_linux_arm64.deb.statement.json");
const signature = new File(["sig"], "frigate_1.4.0_linux_arm64.deb.statement.sig");
const publicKey = new File(["pem"], "frigate-release-2026.pem");

describe("sortReleaseFiles", () => {
  it("sorts a selection by extension, whatever its order", () => {
    expect(sortReleaseFiles(kinds, [signature, publicKey, deb, statement])).toEqual({
      files: { deb, statement, signature, publicKey },
    });
  });

  it("ignores the case of the extension", () => {
    const upper = new File(["deb"], "FRIGATE.DEB");
    expect(sortReleaseFiles(kinds, [upper])).toEqual({ files: { deb: upper } });
  });

  it("refuses a file of a kind it doesn't know", () => {
    expect(sortReleaseFiles(kinds, [deb, new File(["x"], "notes.txt")])).toEqual({
      error: "notes.txt is not a supported file type.",
    });
    // The three package kinds alone don't take a public key.
    expect(sortReleaseFiles(RELEASE_PACKAGE_FILE_KINDS, [publicKey])).toEqual({
      error: "frigate-release-2026.pem is not a supported file type.",
    });
  });

  it("refuses two files of one kind", () => {
    expect(sortReleaseFiles(kinds, [deb, new File(["other"], "frigate_1.4.1_linux_arm64.deb")])).toEqual({
      error: "Select only one package.",
    });
  });
});

describe("missingReleaseFiles", () => {
  it("names the required kinds without a file, and never an optional one", () => {
    expect(missingReleaseFiles(kinds, {})).toEqual(["package", "release statement", "release signature"]);
    expect(missingReleaseFiles(kinds, { deb, signature })).toEqual(["release statement"]);
    expect(missingReleaseFiles(kinds, { deb, statement, signature })).toEqual([]);
  });
});

describe("ReleaseFilePicker", () => {
  const onChange = vi.fn();
  const onReject = vi.fn();
  const renderPicker = (files: ReleaseFiles<Key> = {}, disabled = false) =>
    render(
      <ReleaseFilePicker
        kinds={kinds}
        files={files}
        onChange={onChange}
        onReject={onReject}
        emptyHint="Select the release files."
        inputLabel="Release files"
        disabled={disabled}
      />,
    );
  const input = () => screen.getByLabelText("Release files");
  const select = (...files: File[]) => fireEvent.change(input(), { target: { files } });

  beforeEach(() => vi.clearAllMocks());

  it("offers one input for several files, filtered to the kinds' extensions", () => {
    renderPicker();

    expect(input()).toHaveAttribute("multiple");
    expect(input()).toHaveAttribute("accept", ".deb,.json,.sig,.pem");
    expect(screen.getByRole("status")).toHaveTextContent("Select the release files.");
    expect(screen.queryByRole("button", { name: "Remove" })).not.toBeInTheDocument();
  });

  it("shows each file under its kind's label, in the order of the kinds", () => {
    renderPicker({ publicKey, signature, deb, statement });

    const labels = ["Debian package (.deb)", "Release statement", "Release signature", "Public key"].map((label) =>
      screen.getByText(label),
    );
    for (let index = 1; index < labels.length; index++) {
      expect(labels[index - 1].compareDocumentPosition(labels[index]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    expect(screen.getByRole("status")).toHaveTextContent("All required files selected.");
  });

  it("says which required files are still needed", () => {
    renderPicker({ deb });
    expect(screen.getByRole("status")).toHaveTextContent("Still needed: release statement, release signature.");
  });

  it("adds a selection to the chosen files and replaces a file of the same kind", () => {
    renderPicker({ deb, statement });
    const newer = new File(["newer"], "frigate_1.4.1_linux_arm64.deb");

    select(signature, newer);

    expect(onChange).toHaveBeenCalledExactlyOnceWith({ deb: newer, statement, signature });
    expect(onReject).not.toHaveBeenCalled();
  });

  it("reports a selection it can't use and changes nothing", () => {
    renderPicker({ deb });

    select(statement, new File(["x"], "notes.txt"));

    expect(onReject).toHaveBeenCalledExactlyOnceWith("notes.txt is not a supported file type.");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("removes one file and keeps the others", () => {
    renderPicker({ deb, statement, signature });

    fireEvent.click(screen.getAllByRole("button", { name: "Remove" })[1]);

    expect(onChange).toHaveBeenCalledExactlyOnceWith({ deb, statement: undefined, signature });
  });

  it("keeps the files shown but unchangeable while disabled", () => {
    renderPicker({ deb }, true);

    expect(screen.getByText(deb.name)).toBeInTheDocument();
    expect(input()).toBeDisabled();
    expect(screen.getByRole("button", { name: "Choose files" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();
  });
});
