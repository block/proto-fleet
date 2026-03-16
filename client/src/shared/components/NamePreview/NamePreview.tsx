import { type ReactNode } from "react";
import PreviewContainer from "./PreviewContainer";
import { ArrowDown } from "@/shared/assets/icons";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";

const previewModes = {
  transition: "transition",
  newNameOnly: "new-name-only",
} as const;

export type NamePreviewMode = (typeof previewModes)[keyof typeof previewModes];

const previewLayouts = {
  card: "card",
  inline: "inline",
} as const;

export type NamePreviewLayout = (typeof previewLayouts)[keyof typeof previewLayouts];

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

type NamePreviewProps = (TransitionNamePreviewProps | NewNameOnlyPreviewProps) & {
  layout?: NamePreviewLayout;
};

const NamePreview = (props: NamePreviewProps) => {
  const trimmedNewName = props.newName.trim();
  const hasNewName = trimmedNewName !== "";
  const layout = props.layout ?? previewLayouts.card;
  const isInlineLayout = layout === previewLayouts.inline;

  const getLayoutClassName = (inlineClassName: string, cardClassName: string) =>
    isInlineLayout ? inlineClassName : cardClassName;

  const wrapContent = (children: ReactNode) => {
    if (isInlineLayout) {
      return children;
    }

    return <PreviewContainer>{children}</PreviewContainer>;
  };

  if (props.mode === previewModes.newNameOnly) {
    return wrapContent(
      <>
        {hasNewName ? (
          <span
            className={getLayoutClassName(
              "col-span-3 justify-self-center text-300 text-text-primary",
              "text-300 whitespace-nowrap text-text-primary",
            )}
          >
            {props.newName}
          </span>
        ) : (
          <span
            className={getLayoutClassName(
              "col-span-3 justify-self-center text-300 text-text-primary-30",
              "text-300 whitespace-nowrap text-text-primary-30",
            )}
          >
            {INACTIVE_PLACEHOLDER}
          </span>
        )}
      </>,
    );
  }

  const { currentName } = props;
  const isUnchangedName = trimmedNewName === currentName.trim();
  const showTransition = isInlineLayout || trimmedNewName !== currentName.trim();
  const transitionClassName = getLayoutClassName(
    "contents",
    "flex items-center justify-center gap-6 whitespace-nowrap",
  );
  const showInactivePlaceholder = !hasNewName || (isInlineLayout && isUnchangedName);

  return wrapContent(
    <>
      {showTransition ? (
        <div className={transitionClassName}>
          <span
            className={getLayoutClassName(
              "min-w-0 justify-self-end text-right text-300 [overflow-wrap:anywhere] text-text-primary",
              "text-300 text-text-primary",
            )}
          >
            {currentName}
          </span>
          <ArrowDown
            className={getLayoutClassName(
              "shrink-0 -rotate-90 justify-self-center text-text-primary-30",
              "shrink-0 -rotate-90 text-text-primary-30",
            )}
            width="w-4"
          />
          {!showInactivePlaceholder ? (
            <span
              className={getLayoutClassName(
                "min-w-0 justify-self-start text-left text-300 [overflow-wrap:anywhere] text-text-primary",
                "text-300 text-text-primary",
              )}
              data-testid="active-new-name"
            >
              {props.newName}
            </span>
          ) : (
            <span
              className={getLayoutClassName(
                "min-w-0 justify-self-start text-left text-300 [overflow-wrap:anywhere] text-text-primary-30",
                "text-300 text-text-primary-30",
              )}
            >
              {INACTIVE_PLACEHOLDER}
            </span>
          )}
        </div>
      ) : (
        <span className="text-300 whitespace-nowrap text-text-primary">{currentName}</span>
      )}
    </>,
  );
};

export default NamePreview;
