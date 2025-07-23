import { useCallback, useState } from "react";
import clsx from "clsx";

import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import PageOverlay, {
  animationDuration,
} from "@/shared/components/PageOverlay";

interface MinerFrameHeaderProps {
  title?: string;
  onDismiss: () => void;
}
const MinerFrameHeader = ({ title, onDismiss }: MinerFrameHeaderProps) => {
  return (
    <div className="absolute top-0 z-30 !w-fit bg-surface-elevated-base px-4 py-2.5">
      <div className="flex items-center gap-4">
        <Button
          variant={variants.secondary}
          size={sizes.base}
          prefixIcon={<Dismiss />}
          onClick={onDismiss}
          testId="header-icon-button"
        />
        {title ? (
          <div className="hidden text-heading-200 text-text-primary md:block">
            {title}
          </div>
        ) : null}
      </div>
    </div>
  );
};

interface MinerFrameProps {
  src: string;
  title?: string;
  className?: string;
  show?: boolean;
  onDismiss?: () => void;
}

const MinerFrame = ({
  src,
  title = "",
  className = "",
  show = true,
  onDismiss,
}: MinerFrameProps) => {
  const [showFrame, setShowFrame] = useState(show);

  const closeFrame = useCallback(() => {
    setShowFrame(false);
    if (onDismiss) {
      setTimeout(() => {
        onDismiss();
      }, animationDuration);
    }
  }, [onDismiss]);

  return (
    <PageOverlay show={showFrame} shouldPreventScroll>
      <div
        className={clsx(
          "h-full w-full max-w-full overflow-y-auto rounded-none bg-surface-elevated-base",
          {
            "animate-sliding-up": showFrame,
            "animate-sliding-down": !showFrame,
          },
          className,
        )}
        data-testid="miner-frame"
      >
        <MinerFrameHeader title={title} onDismiss={closeFrame} />
        <div className="h-full w-full pt-0">
          <iframe
            src={src}
            title={title}
            className="h-full w-full border-0"
            style={{ display: "block" }}
            allowFullScreen
          />
        </div>
      </div>
    </PageOverlay>
  );
};

export default MinerFrame;
