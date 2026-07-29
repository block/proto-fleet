import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { ReleaseInfo } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import type { UpdatePillData } from "@/protoFleet/components/PageHeader/PageHeader";
import { useUpdateStatus } from "@/protoFleet/features/updates/api/useUpdateStatus";
import { DISMISSED_UPDATE_TAG_KEY } from "@/protoFleet/features/updates/constants";
import { pushToast, removeToast, STATUSES } from "@/shared/features/toaster";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

export interface UpdateNotificationState {
  closeModal: () => void;
  installCommand: string;
  modalOpen: boolean;
  release: ReleaseInfo | undefined;
  updatePill: UpdatePillData | null;
}

export function useUpdateNotification(): UpdateNotificationState {
  const { status, hasUpdatePermission } = useUpdateStatus();
  const [dismissedTag, setDismissedTag] = useReactiveLocalStorage<string | undefined>(DISMISSED_UPDATE_TAG_KEY);
  const [modalOpen, setModalOpen] = useState(false);
  const toastIdRef = useRef<number | null>(null);
  const toastTagRef = useRef<string | null>(null);

  const release = hasUpdatePermission && status?.updateAvailable ? status.latestEligible : undefined;
  const installCommand = release ? (status?.installCommand ?? "") : "";
  const releaseTag = release?.version;
  const showPill = Boolean(releaseTag && installCommand && releaseTag === dismissedTag);

  const clearToast = useCallback(() => {
    removeToast(toastIdRef.current);
    toastIdRef.current = null;
    toastTagRef.current = null;
  }, []);

  const openModal = useCallback(() => {
    setModalOpen(true);
  }, []);

  const closeModal = useCallback(() => {
    setModalOpen(false);
  }, []);

  useEffect(() => {
    if (!releaseTag || !installCommand || releaseTag === dismissedTag) {
      clearToast();
      return;
    }

    if (toastTagRef.current === releaseTag) {
      return;
    }

    clearToast();
    toastTagRef.current = releaseTag;
    toastIdRef.current = pushToast({
      message: `Update available: Fleet ${releaseTag}`,
      status: STATUSES.info,
      ttl: false,
      onClick: openModal,
      onClose: () => {
        setDismissedTag(releaseTag);
        toastIdRef.current = null;
        toastTagRef.current = null;
      },
    });
  }, [clearToast, dismissedTag, installCommand, openModal, releaseTag, setDismissedTag]);

  useEffect(() => clearToast, [clearToast]);

  const updatePill = useMemo<UpdatePillData | null>(() => {
    if (!showPill || !releaseTag) {
      return null;
    }

    return {
      version: releaseTag,
      onClick: openModal,
    };
  }, [openModal, releaseTag, showPill]);

  return {
    closeModal,
    installCommand,
    modalOpen,
    release,
    updatePill,
  };
}
