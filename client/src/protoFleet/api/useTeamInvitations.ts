import { useCallback } from "react";

import { invitationClient } from "@/protoFleet/api/clients";
import type {
  CreateTeamInvitationRequest,
  DeleteTeamInvitationRequest,
  RevokeTeamInvitationRequest,
  TeamInvitation,
} from "@/protoFleet/api/generated/invitation/v1/invitation_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAbortError } from "@/protoFleet/api/requestErrors";
import { useAuthErrors } from "@/protoFleet/store";

interface CreateTeamInvitationProps {
  roleId: CreateTeamInvitationRequest["roleId"];
  // Zero uses the server default of 24 hours, which is also the maximum.
  expiresInSeconds: CreateTeamInvitationRequest["expiresInSeconds"];
  onSuccess?: (token: string) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListTeamInvitationsProps {
  pageToken?: string;
  signal?: AbortSignal;
  onSuccess?: (invitations: TeamInvitation[], nextPageToken: string) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface RevokeTeamInvitationProps {
  invitationId: RevokeTeamInvitationRequest["invitationId"];
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DeleteTeamInvitationProps {
  invitationId: DeleteTeamInvitationRequest["invitationId"];
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const TEAM_INVITATION_PAGE_SIZE = 50;

const useTeamInvitations = () => {
  const { handleAuthErrors } = useAuthErrors();

  const reportError = useCallback(
    (err: unknown, onError?: (message: string) => void) =>
      handleAuthErrors({
        error: err,
        onError: () => {
          onError?.(getErrorMessage(err));
        },
      }),
    [handleAuthErrors],
  );

  const createTeamInvitation = useCallback(
    async ({ roleId, expiresInSeconds, onSuccess, onError, onFinally }: CreateTeamInvitationProps) => {
      await invitationClient
        .createTeamInvitation({ roleId, expiresInSeconds })
        .then((response) => {
          onSuccess?.(response.token);
        })
        .catch((err) => reportError(err, onError))
        .finally(() => {
          onFinally?.();
        });
    },
    [reportError],
  );

  const listTeamInvitations = useCallback(
    async ({ pageToken = "", signal, onSuccess, onError, onFinally }: ListTeamInvitationsProps) => {
      await invitationClient
        .listTeamInvitations({ pageSize: TEAM_INVITATION_PAGE_SIZE, pageToken }, signal ? { signal } : undefined)
        .then((response) => {
          onSuccess?.(response.invitations, response.nextPageToken);
        })
        .catch((err) => {
          if (!isAbortError(err, signal)) reportError(err, onError);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [reportError],
  );

  const revokeTeamInvitation = useCallback(
    async ({ invitationId, onSuccess, onError, onFinally }: RevokeTeamInvitationProps) => {
      await invitationClient
        .revokeTeamInvitation({ invitationId })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => reportError(err, onError))
        .finally(() => {
          onFinally?.();
        });
    },
    [reportError],
  );

  const deleteTeamInvitation = useCallback(
    async ({ invitationId, onSuccess, onError, onFinally }: DeleteTeamInvitationProps) => {
      await invitationClient
        .deleteTeamInvitation({ invitationId })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => reportError(err, onError))
        .finally(() => {
          onFinally?.();
        });
    },
    [reportError],
  );

  return { createTeamInvitation, listTeamInvitations, revokeTeamInvitation, deleteTeamInvitation };
};

export type UseTeamInvitationsReturn = ReturnType<typeof useTeamInvitations>;

export { useTeamInvitations };
