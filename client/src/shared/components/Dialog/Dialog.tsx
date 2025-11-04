import { ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import ButtonGroup from "@/shared/components/ButtonGroup";
import { groupVariants } from "@/shared/components/ButtonGroup/constants";
import { ButtonProps } from "@/shared/components/ButtonGroup/types";
import Header from "@/shared/components/Header";
import PageOverlay, {
  animationDuration,
} from "@/shared/components/PageOverlay";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface DialogProps {
  className?: string;
  children?: ReactNode;
  icon?: ReactNode;
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
  buttonGroupVariant?: keyof typeof groupVariants;
  buttons?: ButtonProps[];
}

const Dialog = ({
  className,
  children,
  icon,
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
  buttonGroupVariant = groupVariants.justifyBetween,
  buttons,
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
      // eslint-disable-next-line react-hooks/set-state-in-effect
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
              "h-fit w-[360px] overflow-hidden rounded-3xl bg-surface-elevated-base shadow-200",
              {
                "animate-sliding-up": animate && show,
                "animate-sliding-down": animate && !show,
              },
              className,
            )}
            data-testid={testId}
          >
            <div className="p-6">
              <div className="flex flex-col gap-3">
                {loading ? (
                  <ProgressCircular
                    indeterminate
                    className="text-core-accent-fill"
                  />
                ) : icon ? (
                  icon
                ) : null}
                <Header
                  className={headerClassName}
                  subtitleClassName={subtitleClassName}
                  title={title}
                  subtitle={subtitle}
                  titleSize={titleSize}
                  subtitleSize={subtitleSize}
                />
              </div>
              {children && <div className="mt-4">{children}</div>}
            </div>
            {buttons && buttons.length > 0 && (
              <ButtonGroup
                buttons={buttons}
                variant={buttonGroupVariant}
                className="rounded-b-3xl bg-surface-5 p-6"
              />
            )}
          </div>
        </PageOverlay>
      )}
    </>
  );
};

export default Dialog;
