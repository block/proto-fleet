import Button, { variants } from "@/shared/components/Button";
import { formatFileSize } from "@/shared/components/FileSizeValue";

interface FileSelectedStatusProps {
  fileName: string;
  fileSize: number;
  onRemove: () => void;
  /** Keeps the file shown but stops it being removed, for example while it is being read. */
  disabled?: boolean;
}

/** A file the user selected, with its size and a way to remove it. */
export default function FileSelectedStatus({ fileName, fileSize, onRemove, disabled }: FileSelectedStatusProps) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border border-border-5 p-4">
      <div className="flex min-w-0 flex-col">
        <div className="truncate text-300 text-text-primary" title={fileName}>
          {fileName}
        </div>
        <div className="text-200 text-text-primary-70">{formatFileSize(fileSize)}</div>
      </div>
      <Button
        variant={variants.textOnly}
        textColor="text-core-accent-fill"
        textOnlyUnderlineOnHover={false}
        className="shrink-0"
        disabled={disabled}
        onClick={onRemove}
        text="Remove"
      />
    </div>
  );
}
