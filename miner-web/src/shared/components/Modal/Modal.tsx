import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { Dismiss } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";

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
  contentHeader?: string;
  onDismiss?: (buttonClicked?: boolean) => void;
  buttons?: ModalButtonProps[];
  show?: boolean;
  showHeader?: boolean;
  title?: string;
}

const Modal = ({
  children,
  className,
  contentHeader,
  onDismiss,
  buttons,
  show = true,
  showHeader = true,
  title,
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
        !button?.dismissModalOnClick
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

  useClickOutside({ ref: ModalRef, onClickOutside: dismissModal });

  return (
    <PageOverlay show={showModal}>
      <div
        className={clsx(
          "shadow-300 rounded-3xl p-6 w-[640px] h-fit bg-surface-elevated-base",
          {
            "animate-sliding-up": showModal,
            "animate-sliding-down": !showModal,
          },
          className,
        )}
        ref={ModalRef}
        data-testid="modal"
      >
        {showHeader && (
          <>
            <Header
              title={title}
              titleSize="text-heading-200"
              icon={<Dismiss />}
              iconOnClick={dismissModal}
              buttons={buttons?.map((button) => ({
                ...button,
                onClick: onButtonClick(button),
              }))}
              inline
            />
            <Divider className="my-6" />
          </>
        )}
        {contentHeader && (
          <div className="text-heading-200 text-text-primary mb-1">
            {contentHeader}
          </div>
        )}
        <div className="text-300 text-text-primary-70">{children}</div>
      </div>
    </PageOverlay>
  );
};

export default Modal;
