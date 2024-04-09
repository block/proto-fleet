import { ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import Header from "components/Header";
import PageOverlay, { animationDuration } from "components/PageOverlay";
import Spinner from "components/Spinner";

interface DialogProps {
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
    }
  }, [show]);

  return (
    <>
      {showDialog && (
        <PageOverlay zIndex="z-40" shouldPreventScroll={preventScroll} show={show}>
          <div
            className={clsx(
              "shadow-200 rounded-3xl p-6 w-[360px] h-fit bg-surface-base",
              {
                "animate-sliding-up": show,
                "animate-sliding-down": !show,
              }
            )}
            data-testid={testId}
          >
            {loading && <Spinner className="h-6 mb-3" />}
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
