import { useRef } from "react";
import { missingReleaseFiles, type ReleaseFileKind, type ReleaseFiles, sortReleaseFiles } from "./releaseFiles";
import Button, { variants } from "@/shared/components/Button";
import FileSelectedStatus from "@/shared/components/FileSelectedStatus";

interface ReleaseFilePickerProps<Key extends string> {
  kinds: ReleaseFileKind<Key>[];
  files: ReleaseFiles<Key>;
  /** The files after a selection was added or a file was removed. */
  onChange: (files: ReleaseFiles<Key>) => void;
  /** A selection that can't be used. The chosen files stay as they were. */
  onReject: (message: string) => void;
  /** What to select, shown while no file is chosen. */
  emptyHint: string;
  /** The accessible name of the file input. */
  inputLabel: string;
  disabled?: boolean;
}

/**
 * One picker for all the files of a controller software release. Each file is shown under the label of its kind.
 * A new selection adds to the chosen files and replaces a file of the same kind, so a file that was left out can
 * be added without choosing all of them again.
 */
export default function ReleaseFilePicker<Key extends string>({
  kinds,
  files,
  onChange,
  onReject,
  emptyHint,
  inputLabel,
  disabled,
}: ReleaseFilePickerProps<Key>) {
  const input = useRef<HTMLInputElement>(null);
  const chosen = kinds.filter((kind) => files[kind.key]);
  const missing = missingReleaseFiles(kinds, files);
  let hint = emptyHint;
  if (chosen.length > 0) {
    hint = missing.length === 0 ? "All required files selected." : `Still needed: ${missing.join(", ")}.`;
  }

  const choose = (selected: File[]) => {
    if (selected.length === 0) return;
    const sorted = sortReleaseFiles(kinds, selected);
    if ("error" in sorted) onReject(sorted.error);
    else onChange({ ...files, ...sorted.files });
  };

  return (
    <>
      <div className="flex items-center justify-between gap-4 rounded-lg border border-dashed border-border-5 p-4">
        <span role="status" className="text-200 text-text-primary-70">
          {hint}
        </span>
        <Button
          variant={variants.secondary}
          text="Choose files"
          className="shrink-0"
          disabled={disabled}
          onClick={() => input.current?.click()}
        />
      </div>
      <input
        ref={input}
        type="file"
        multiple
        accept={kinds.map((kind) => kind.extension).join(",")}
        aria-label={inputLabel}
        className="hidden"
        disabled={disabled}
        onChange={(event) => {
          choose(Array.from(event.target.files ?? []));
          event.target.value = "";
        }}
      />
      {chosen.map(({ key, label }) => {
        const file = files[key];
        return file ? (
          <div key={key} className="flex flex-col gap-2">
            <div className="text-emphasis-300 text-text-primary">{label}</div>
            <FileSelectedStatus
              fileName={file.name}
              fileSize={file.size}
              disabled={disabled}
              onRemove={() => onChange({ ...files, [key]: undefined })}
            />
          </div>
        ) : null;
      })}
    </>
  );
}
