import { ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import Header from "@/shared/components/Header";
import PageOverlay, {
  animationDuration,
} from "@/shared/components/PageOverlay";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface DialogProps {
  className?: string;
  children?: ReactNode;
  loading?: boolean;
  preventScroll?: boolean;
  show: boolean;
  subtitle?: string;
  subtitleClassName?: string;
  subtitleSize?: string;
  testId?: string;
  title: string;
  titleSize?: string;
  headerClassName?: string;
  animate?: boolean;
}

const Dialog = ({
  className,
  children,
  loading,
  preventScroll,
  show,
  subtitle,
  subtitleClassName,
  subtitleSize = "text-heading-100",
  testId,
  title,
  titleSize = "text-heading-100",
  headerClassName,
  animate = true,
}: DialogProps) => {
  const [showDialog, setShowDialog] = useState(show);

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    if (!show && animate) {
      // Wait for the animation to finish before hiding the dialog
      timeoutId = setTimeout(() => {
        setShowDialog(show);
      }, animationDuration);
    } else {
      setShowDialog(show);
    }

    return () => {
      clearTimeout(timeoutId);
    };
  }, [animate, show]);

  return (
    <>
      {showDialog && (
        <PageOverlay
          zIndex="z-40"
          shouldPreventScroll={preventScroll}
          show={show}
          animate={animate}
        >
          <div
            className={clsx(
              "h-fit w-[360px] overflow-hidden rounded-3xl bg-surface-elevated-base p-6 shadow-200",
              {
                "animate-sliding-up": animate && show,
                "animate-sliding-down": animate && !show,
              },
              className,
            )}
            data-testid={testId}
          >
            {loading && (
              <ProgressCircular
                indeterminate
                className="mb-3 h-6 text-core-accent-fill"
              />
            )}
            <Header
              className={headerClassName}
              subtitleClassName={subtitleClassName}
              title={title}
              subtitle={subtitle}
              titleSize={titleSize}
              subtitleSize={subtitleSize}
            />
            {children && <div className="mt-4">{children}</div>}
          </div>
        </PageOverlay>
      )}
    </>
  );
};

export default Dialog;
