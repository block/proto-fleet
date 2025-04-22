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
  subtitleSize?: string;
  testId?: string;
  title: string;
  titleSize?: string;
}

const Dialog = ({
  className,
  children,
  loading,
  preventScroll,
  show,
  subtitle,
  subtitleSize = "text-heading-100",
  testId,
  title,
  titleSize = "text-heading-100",
}: DialogProps) => {
  const [showDialog, setShowDialog] = useState(show);

  useEffect(() => {
    if (!show) {
      // Wait for the animation to finish before hiding the dialog
      setTimeout(() => {
        setShowDialog(show);
      }, animationDuration);
    } else {
      setShowDialog(show);
    }
  }, [show]);

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    if (show) {
      setShowDialog(true);
    } else {
      // Wait for the animation to finish before hiding the dialog
      timeoutId = setTimeout(() => {
        setShowDialog(false);
      }, animationDuration);
    }
    return () => {
      // clear timeout if the component is unmounted before the timeout
      clearTimeout(timeoutId);
    };
  }, [show]);

  return (
    <>
      {showDialog && (
        <PageOverlay
          zIndex="z-40"
          shouldPreventScroll={preventScroll}
          show={show}
        >
          <div
            className={clsx(
              "h-fit w-[360px] rounded-3xl bg-surface-elevated-base p-6 shadow-200",
              {
                "animate-sliding-up": show,
                "animate-sliding-down": !show,
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
              title={title}
              subtitle={subtitle}
              titleSize={titleSize}
              subtitleSize={subtitleSize}
            />
            <div className="mt-4">{children}</div>
          </div>
        </PageOverlay>
      )}
    </>
  );
};

export default Dialog;
