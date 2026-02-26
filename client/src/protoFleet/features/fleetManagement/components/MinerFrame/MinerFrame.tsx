import { motion } from "motion/react";
import { useCallback } from "react";
import clsx from "clsx";

import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import PageOverlay from "@/shared/components/PageOverlay";
import useSlideUpAnimation from "@/shared/hooks/useSlideUpAnimation";

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
        {title ? <div className="hidden text-heading-200 text-text-primary md:block">{title}</div> : null}
      </div>
    </div>
  );
};

interface MinerFrameProps {
  open?: boolean;
  src: string;
  title?: string;
  className?: string;
  onDismiss?: () => void;
}

const MinerFrame = ({ open, src, title = "", className = "", onDismiss }: MinerFrameProps) => {
  const slideUpAnimation = useSlideUpAnimation();

  const closeFrame = useCallback(() => {
    onDismiss?.();
  }, [onDismiss]);

  return (
    <PageOverlay open={open} shouldPreventScroll>
      <motion.div
        {...slideUpAnimation}
        className={clsx("h-full w-full max-w-full overflow-y-auto rounded-none bg-surface-elevated-base", className)}
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
      </motion.div>
    </PageOverlay>
  );
};

export default MinerFrame;
