import { PreviewContainer } from "@/shared/components/NamePreview";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";

interface HighlightedNamePreviewProps {
  previewName: string;
  highlightedText?: string;
  highlightStartIndex?: number;
  testIdPrefix?: string;
}

interface HighlightedPreviewSections {
  leading: string;
  highlighted: string;
  trailing: string;
}

const defaultTestIdPrefix = "highlighted-name-preview";

const buildHighlightedSections = (
  previewName: string,
  highlightedText: string,
  highlightStartIndex?: number,
): HighlightedPreviewSections => {
  if (previewName === "" || highlightedText === "") {
    return { leading: "", highlighted: previewName, trailing: "" };
  }

  const explicitHighlightMatches =
    typeof highlightStartIndex === "number" &&
    highlightStartIndex >= 0 &&
    previewName.slice(highlightStartIndex, highlightStartIndex + highlightedText.length) === highlightedText;

  const index = explicitHighlightMatches ? highlightStartIndex : previewName.indexOf(highlightedText);

  if (index === -1) {
    return { leading: "", highlighted: previewName, trailing: "" };
  }

  return {
    leading: previewName.slice(0, index),
    highlighted: highlightedText,
    trailing: previewName.slice(index + highlightedText.length),
  };
};

const HighlightedNamePreview = ({
  previewName,
  highlightedText = previewName,
  highlightStartIndex,
  testIdPrefix = defaultTestIdPrefix,
}: HighlightedNamePreviewProps) => {
  const previewSections = buildHighlightedSections(previewName, highlightedText, highlightStartIndex);

  return (
    <PreviewContainer>
      {previewName === "" ? (
        <span className="text-300 whitespace-nowrap text-text-primary-30">{INACTIVE_PLACEHOLDER}</span>
      ) : (
        <span className="text-300 whitespace-nowrap text-text-primary-30" data-testid={testIdPrefix}>
          <span data-testid={`${testIdPrefix}-leading`}>{previewSections.leading}</span>
          <span className="text-text-primary" data-testid={`${testIdPrefix}-highlighted`}>
            {previewSections.highlighted}
          </span>
          <span data-testid={`${testIdPrefix}-trailing`}>{previewSections.trailing}</span>
        </span>
      )}
    </PreviewContainer>
  );
};

export default HighlightedNamePreview;
