import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { useClickOutside } from "common/hooks/useClickOutside";

import { variants } from "components/Button";
import { ButtonProps } from "components/ButtonGroup";
import Divider from "components/Divider";
import Header from "components/Header";
import PageOverlay, { animationDuration } from "components/PageOverlay";

import { Dismiss } from "icons";

// optional prop to delay close modal on clicking button and allow animations to finish
interface ModalButtonProps extends ButtonProps {
  dismissModalOnClick?: boolean;
}

interface ModalProps {
  children: ReactNode;
  contentHeader?: string;
  onDismiss: (buttonClicked?: boolean) => void;
  buttons?: ModalButtonProps[];
  show?: boolean;
  title?: string;
}

const Modal = ({
  children,
  contentHeader,
  onDismiss,
  buttons,
  show = true,
  title,
}: ModalProps) => {
  const [showModal, setShowModal] = useState(show);
  const ModalRef = useRef<HTMLDivElement>(null);

  const closeModal = useCallback(
    (buttonClicked?: boolean) => {
      setShowModal(false);
      setTimeout(() => {
        onDismiss(buttonClicked);
      }, animationDuration);
    },
    [onDismiss]
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
    [closeModal]
  );

  const dismissOnEsc = useCallback(
    (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closeModal();
      }
    },
    [closeModal]
  );

  useEffect(() => {
    document.addEventListener("keydown", dismissOnEsc, false);

    return () => {
      document.removeEventListener("keydown", dismissOnEsc, false);
    };
  }, [dismissOnEsc]);

  const onClickOutside = useCallback(() => {
    closeModal();
  }, [closeModal]);

  useClickOutside({ ref: ModalRef, onClickOutside });

  return (
    <PageOverlay show={showModal}>
      <div
        className={clsx(
          "shadow-300 rounded-3xl p-6 w-[640px] h-fit bg-surface-base",
          {
            "animate-sliding-up": showModal,
            "animate-sliding-down": !showModal,
          }
        )}
        ref={ModalRef}
        data-testid="modal"
      >
        <Header
          title={title}
          icon={<Dismiss />}
          iconOnClick={onClickOutside}
          buttons={buttons?.map((button) => ({
            ...button,
            onClick: onButtonClick(button),
          }))}
          inline
        />
        <Divider className="my-6" />
        {contentHeader && (
          <div className="text-heading-200 text-text-primary mb-1">
            {contentHeader}
          </div>
        )}
        <div className="text-300 text-text-primary/70">{children}</div>
      </div>
    </PageOverlay>
  );
};

export default Modal;
