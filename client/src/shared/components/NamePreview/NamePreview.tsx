import PreviewContainer from "./PreviewContainer";
import { ArrowDown } from "@/shared/assets/icons";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";

const previewModes = {
  transition: "transition",
  newNameOnly: "new-name-only",
} as const;

export type NamePreviewMode = (typeof previewModes)[keyof typeof previewModes];

interface TransitionNamePreviewProps {
  mode?: typeof previewModes.transition;
  currentName: string;
  newName: string;
}

interface NewNameOnlyPreviewProps {
  mode: typeof previewModes.newNameOnly;
  currentName?: never;
  newName: string;
}

type NamePreviewProps = TransitionNamePreviewProps | NewNameOnlyPreviewProps;

const NamePreview = (props: NamePreviewProps) => {
  const trimmedNewName = props.newName.trim();
  const hasNewName = trimmedNewName !== "";

  if (props.mode === previewModes.newNameOnly) {
    return (
      <PreviewContainer>
        {hasNewName ? (
          <span className="text-300 whitespace-nowrap text-text-primary">{props.newName}</span>
        ) : (
          <span className="text-300 whitespace-nowrap text-text-primary-30">{INACTIVE_PLACEHOLDER}</span>
        )}
      </PreviewContainer>
    );
  }

  const { currentName } = props;
  const showTransition = trimmedNewName !== currentName.trim();

  return (
    <PreviewContainer>
      {showTransition ? (
        <div className="flex items-center justify-center gap-6 whitespace-nowrap">
          <span className="text-300 text-text-primary">{currentName}</span>
          <ArrowDown className="shrink-0 -rotate-90 text-text-primary-30" width="w-4" />
          {hasNewName ? (
            <span className="text-300 text-text-primary">{props.newName}</span>
          ) : (
            <span className="text-300 text-text-primary-30">{INACTIVE_PLACEHOLDER}</span>
          )}
        </div>
      ) : (
        <span className="text-300 whitespace-nowrap text-text-primary">{currentName}</span>
      )}
    </PreviewContainer>
  );
};

export default NamePreview;
