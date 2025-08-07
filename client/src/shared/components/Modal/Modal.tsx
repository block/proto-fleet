import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { sizes } from "./constants";
import { Dismiss } from "@/shared/assets/icons";
import { sizes as buttonSizes, variants } from "@/shared/components/Button";

import { ButtonProps } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import PageOverlay, {
  animationDuration,
} from "@/shared/components/PageOverlay";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

// optional prop to delay close modal on clicking button and allow animations to finish
interface ModalButtonProps extends ButtonProps {
  dismissModalOnClick?: boolean;
}

interface ModalProps {
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
  contentHeader?: string;
  onDismiss?: (buttonClicked?: boolean) => void;
  buttonSize?: keyof typeof buttonSizes;
  buttons?: ModalButtonProps[];
  show?: boolean;
  showHeader?: boolean;
  title?: string;
  description?: string;
  preventClose?: boolean;
  divider?: boolean;
  size?: keyof typeof sizes;
}

const Modal = ({
  children,
  className,
  bodyClassName,
  contentHeader,
  onDismiss,
  buttonSize,
  buttons,
  show = true,
  showHeader = true,
  title,
  description,
  preventClose,
  divider = true,
  size = sizes.large,
}: ModalProps) => {
  const [showModal, setShowModal] = useState(show);
  const ModalRef = useRef<HTMLDivElement>(null);

  const closeModal = useCallback(
    (buttonClicked?: boolean) => {
      if (onDismiss === undefined) {
        return;
      }
      setShowModal(false);
      setTimeout(() => {
        onDismiss(buttonClicked);
      }, animationDuration);
    },
    [onDismiss],
  );

  useEffect(() => {
    if (!show) {
      closeModal();
    }
  }, [closeModal, show]);

  // if button is supposed to dismiss modal, animate closing it
  const onButtonClick = useCallback(
    (button?: ModalButtonProps) => () => {
      if (
        button?.variant === variants.primary &&
        button?.dismissModalOnClick !== false
      ) {
        closeModal(true);
      }
      button?.onClick?.();
    },
    [closeModal],
  );

  const dismissModal = useCallback(() => {
    closeModal();
  }, [closeModal]);

  useKeyDown({ key: "Escape", onKeyDown: dismissModal });

  useClickOutside({
    ref: ModalRef,
    onClickOutside: dismissModal,
    ignoreSelectors: [".popover-content"], // Ignore clicks inside popovers
  });

  return (
    <PageOverlay show={showModal}>
      <div
        className={clsx(
          "h-fit w-fit rounded-3xl bg-surface-elevated-base p-6 shadow-300",
          {
            "min-w-256": size === sizes.extraLarge,
            "min-w-160": size === sizes.large,
            "min-w-90": size === sizes.small,
            "animate-sliding-up": showModal,
            "animate-sliding-down": !showModal,
            "max-w-[640px]": size === sizes.small,
            "max-w-[1024px]": size === sizes.large,
            "max-w-[1280px]": size === sizes.extraLarge,
            "h-full w-full max-w-full overflow-y-auto rounded-none":
              size === sizes.fullscreen,
          },
          className,
        )}
        ref={ModalRef}
        data-testid="modal"
      >
        {showHeader && (
          <>
            <Header
              className={clsx({
                "sticky top-0": size === sizes.fullscreen,
              })}
              title={title}
              description={description}
              titleSize="text-heading-200"
              icon={preventClose ? undefined : <Dismiss />}
              iconOnClick={preventClose ? undefined : dismissModal}
              buttonSize={buttonSize}
              buttons={buttons?.map((button) => ({
                ...button,
                onClick: onButtonClick(button),
              }))}
              inline
            />
            {!preventClose && divider && <Divider className="mt-6" />}
            {(preventClose || !divider) && <div className="mt-6" />}
          </>
        )}
        {contentHeader && (
          <div className="mb-1 text-heading-200 text-text-primary">
            {contentHeader}
          </div>
        )}
        <div className={clsx("text-300 text-text-primary-70", bodyClassName)}>
          {children}
        </div>
      </div>
    </PageOverlay>
  );
};

export default Modal;
