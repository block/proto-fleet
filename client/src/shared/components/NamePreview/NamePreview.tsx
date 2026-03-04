import { ArrowDown } from "@/shared/assets/icons";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";

interface NamePreviewProps {
  currentName: string;
  newName: string;
}

const NamePreview = ({ currentName, newName }: NamePreviewProps) => {
  const showTransition = newName.trim() !== currentName.trim();

  return (
    <div className="flex min-h-40 items-center justify-center rounded-3xl bg-black/5 px-3 py-6">
      {showTransition ? (
        <div className="flex w-full min-w-0 items-center justify-center gap-6">
          <span className="max-w-[50%] shrink-0 text-300 break-words text-text-primary">{currentName}</span>
          <ArrowDown className="shrink-0 -rotate-90 text-text-primary-30" width="w-4" />
          {newName.trim() ? (
            <span className="min-w-0 text-300 break-words text-text-primary">{newName}</span>
          ) : (
            <span className="text-300 text-text-primary-30">{INACTIVE_PLACEHOLDER}</span>
          )}
        </div>
      ) : (
        <span className="text-300 break-words text-text-primary">{currentName}</span>
      )}
    </div>
  );
};

export default NamePreview;
