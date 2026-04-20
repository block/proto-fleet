import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { ConnectError } from "@connectrpc/connect";
import { foremanImportClient } from "@/protoFleet/api/clients";
import {
  CompleteImportRequestSchema,
  type CompleteImportResponse,
  ForemanCredentialsSchema,
  ImportFromForemanRequestSchema,
  type ImportFromForemanResponse,
} from "@/protoFleet/api/generated/foremanimport/v1/foremanimport_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

const buildCredentials = (apiKey: string, clientId: string) => create(ForemanCredentialsSchema, { apiKey, clientId });

const useForemanImport = () => {
  const { handleAuthErrors } = useAuthErrors();
  const [importPending, setImportPending] = useState(false);

  const handleRpcError = useCallback(
    (error: unknown, onError?: (m: string) => void) => {
      if (error instanceof ConnectError) {
        handleAuthErrors({ error, onError: () => onError?.(getErrorMessage(error, "An unexpected error occurred")) });
      } else if (error instanceof Error) {
        onError?.(error.message);
      } else {
        onError?.(getErrorMessage(error));
      }
    },
    [handleAuthErrors],
  );

  const importFromForeman = useCallback(
    async (args: {
      apiKey: string;
      clientId: string;
      onSuccess: (r: ImportFromForemanResponse) => void;
      onError?: (m: string) => void;
    }) => {
      setImportPending(true);
      try {
        const response = await foremanImportClient.importFromForeman(
          create(ImportFromForemanRequestSchema, { credentials: buildCredentials(args.apiKey, args.clientId) }),
        );
        args.onSuccess(response);
      } catch (error) {
        handleRpcError(error, args.onError);
      } finally {
        setImportPending(false);
      }
    },
    [handleRpcError],
  );

  const completeImport = useCallback(
    async (args: {
      apiKey: string;
      clientId: string;
      importPools: boolean;
      importGroups: boolean;
      importRacks: boolean;
      pairedDeviceIdentifiers: string[];
      onSuccess: (r: CompleteImportResponse) => void;
      onError?: (m: string) => void;
    }) => {
      try {
        const response = await foremanImportClient.completeImport(
          create(CompleteImportRequestSchema, {
            credentials: buildCredentials(args.apiKey, args.clientId),
            importPools: args.importPools,
            importGroups: args.importGroups,
            importRacks: args.importRacks,
            pairedDeviceIdentifiers: args.pairedDeviceIdentifiers,
          }),
        );
        args.onSuccess(response);
      } catch (error) {
        handleRpcError(error, args.onError);
      }
    },
    [handleRpcError],
  );

  return useMemo(
    () => ({ importPending, importFromForeman, completeImport }),
    [importPending, importFromForeman, completeImport],
  );
};

export { useForemanImport };
