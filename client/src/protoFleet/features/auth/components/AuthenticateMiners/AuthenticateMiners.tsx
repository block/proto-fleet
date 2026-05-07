import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { create } from "@bufbuild/protobuf";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceFilterSchema, DeviceSelectorSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { CredentialsSchema, PairRequestSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { ids } from "@/protoFleet/features/auth/components/AuthenticateMiners/constants";
import { Credentials, UnauthenticatedMiner } from "@/protoFleet/features/auth/components/AuthenticateMiners/types";
import { createModelFilter, filterByModel } from "@/protoFleet/utils/minerFilters";
import { Alert } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button/constants";
import Callout, { intents } from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import { ActiveFilters } from "@/shared/components/List/Filters/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";

import Switch from "@/shared/components/Switch";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

const activeCols = ["model", "ipAddress", "username", "password"] as (keyof UnauthenticatedMiner)[];

const colTitles = {
  model: "Model",
  deviceIdentifier: "ID",
  macAddress: "MAC Address",
  ipAddress: "IP Address",
  username: "Username",
  password: "Password",
} as {
  [key in (typeof activeCols)[number]]: string;
};

type AuthenticateMinersProps = {
  open?: boolean;
  onClose: () => void;
  onSuccess?: () => void;
  onPairingCompleted?: () => void;
  onRefetchMiners?: () => void;
};

const AuthenticateMiners = ({
  open,
  onClose,
  onSuccess,
  onPairingCompleted,
  onRefetchMiners,
}: AuthenticateMinersProps) => {
  const isVisible = open ?? true;
  // Component fetches its own data
  const {
    miners: minersByIdentifier,
    refetch: refetchAuthNeededMiners,
    totalMiners,
  } = useAuthNeededMiners({
    enabled: isVisible,
  });
  const { pair } = useMinerPairing();
  const { refetch: refetchOnboardingStatus } = useOnboardedStatus({ enabled: isVisible });

  // Track if component is mounted to prevent state updates after unmount
  const isMountedRef = useRef(true);

  // Stable reference to track authentication completion across re-renders
  const completionTrackerRef = useRef<{
    completed: number;
    total: number;
    failedMiners: string[];
  }>({
    completed: 0,
    total: 0,
    failedMiners: [],
  });

  useEffect(() => {
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  const [bulkCredentials, setBulkCredentials] = useState<Credentials>({
    username: "",
    password: "",
  });
  // stores credentials for each miner, keyed by deviceIdentifier
  const [credentials, setCredentials] = useState<Record<UnauthenticatedMiner["deviceIdentifier"], Credentials>>({});
  const [hasMissingCredentials, setHasMissingCredentials] = useState(false);
  // stores ids of miners that have errors
  const [minerErrors, setMinerErrors] = useState<UnauthenticatedMiner["deviceIdentifier"][]>([]);
  const [authenticateLoading, setAuthenticateLoading] = useState(false);

  const errorMessage = useMemo(() => {
    if (hasMissingCredentials) {
      return "Enter a username and password and try again.";
    }
    if (minerErrors && minerErrors.length > 0) {
      return "Try your username and password again.";
    }
    return null;
  }, [hasMissingCredentials, minerErrors]);

  const handleBulkChange = useCallback(
    (value: string, id: string) => {
      setBulkCredentials((prev) => ({ ...prev, [id]: value.trim() }));
    },
    [setBulkCredentials],
  );

  const handleMinerChange = useCallback(
    (deviceIdentifier: string, key: string, value: string) => {
      setCredentials((prev) => ({
        ...prev,
        [deviceIdentifier]: {
          ...(prev[deviceIdentifier] || {}),
          [key]: value.trim(),
        },
      }));
    },
    [setCredentials],
  );

  const [showMiners, setShowMiners] = useState(false);
  const [showPasswords, setShowPasswords] = useState(false);

  const minerItems: UnauthenticatedMiner[] = useMemo(() => {
    return Object.values(minersByIdentifier).map((device) => ({
      deviceIdentifier: device.deviceIdentifier,
      model: device.model,
      macAddress: device.macAddress || "",
      ipAddress: device.ipAddress || "",
      username: "",
      password: "",
    }));
  }, [minersByIdentifier]);

  const [selectedMiners, setSelectedMiners] = useState<string[]>([]);
  // Track if we've initialized selection to prevent unwanted resets
  const hasInitializedSelectionRef = useRef(false);
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>({
    buttonFilters: [],
    dropdownFilters: {},
    numericFilters: {},
    textareaListFilters: {},
  });

  useEffect(() => {
    if (!isVisible) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- resetting modal-internal state when parent hides the modal; owner-hoisting would leak modal lifecycle into every caller
      setBulkCredentials({ username: "", password: "" });
      setCredentials({});
      setHasMissingCredentials(false);
      setMinerErrors([]);
      setAuthenticateLoading(false);
      setShowMiners(false);
      setShowPasswords(false);
      // eslint-disable-next-line react-hooks/immutability -- one-shot init guard reset on close; ref is local-only
      hasInitializedSelectionRef.current = false;
    }
  }, [isVisible]);

  // Initialize selection to all miners only on first data load
  // After initial load, preserve user selection even when miner list updates
  useEffect(() => {
    const minerIds = Object.keys(minersByIdentifier);
    if (!hasInitializedSelectionRef.current && minerIds.length > 0) {
      setSelectedMiners(minerIds);
      // eslint-disable-next-line react-hooks/immutability -- one-shot init guard; ref is local-only
      hasInitializedSelectionRef.current = true;
    }
  }, [minersByIdentifier]);

  const models = useMemo(() => {
    return Array.from(new Set(minerItems.map((miner) => miner.model)));
  }, [minerItems]);

  const modelFilter = useMemo(() => createModelFilter(models), [models]);

  const filters = useMemo(() => [modelFilter], [modelFilter]);

  const filteredMiners = useMemo(() => {
    return minerItems.filter((miner) => filterByModel(miner, activeFilters));
  }, [minerItems, activeFilters]);

  const colConfig = useMemo(() => {
    return {
      model: {
        width: "w-40",
      },
      macAddress: {
        width: "w-40",
      },
      username: {
        component: (item: UnauthenticatedMiner) => (
          <Input
            id={item.deviceIdentifier + "_username"}
            className="h-10!"
            label="Username"
            initValue={credentials[item.deviceIdentifier]?.username ?? bulkCredentials.username}
            hideLabelOnFocus
            disabled={
              authenticateLoading
                ? bulkCredentials.username !== "" ||
                  (credentials[item.deviceIdentifier] !== undefined &&
                    credentials[item.deviceIdentifier].username !== "")
                : false
            }
            error={minerErrors.find((id) => id === item.deviceIdentifier) !== undefined}
            onChange={handleMinerChange.bind(this, item.deviceIdentifier, ids.username)}
          />
        ),
        width: "w-70 !py-3",
      },
      password: {
        component: (item: UnauthenticatedMiner) => (
          <Input
            id={item.deviceIdentifier + "_password"}
            className="h-10!"
            label="Password"
            type={showPasswords ? "text" : "password"}
            initValue={credentials[item.deviceIdentifier]?.password ?? bulkCredentials.password}
            hideLabelOnFocus
            disabled={
              authenticateLoading
                ? bulkCredentials.password !== "" ||
                  (credentials[item.deviceIdentifier] !== undefined &&
                    credentials[item.deviceIdentifier].password !== "")
                : false
            }
            error={minerErrors.find((id) => id === item.deviceIdentifier) !== undefined}
            onChange={handleMinerChange.bind(this, item.deviceIdentifier, ids.password)}
          />
        ),
        width: "w-70 !py-3",
      },
    };
  }, [handleMinerChange, bulkCredentials, showPasswords, authenticateLoading, minerErrors, credentials]);

  // Helper to perform common post-authentication operations
  const handleAuthenticationComplete = useCallback(
    (successCount: number) => {
      refetchOnboardingStatus();
      onRefetchMiners?.();
      refetchAuthNeededMiners();
      onPairingCompleted?.();
      if (successCount > 0) {
        onSuccess?.();
      }
    },
    [refetchOnboardingStatus, onRefetchMiners, refetchAuthNeededMiners, onPairingCompleted, onSuccess],
  );

  const authenticateMiners = useCallback(() => {
    if (
      (bulkCredentials.username === "" || bulkCredentials.password === "") &&
      Object.entries(credentials).length === 0
    ) {
      setHasMissingCredentials(true);
      return;
    }

    setHasMissingCredentials(false);
    setAuthenticateLoading(true);

    // Determine if we can use bulk mode (all miners with same credentials)
    const hasIndividualCredentials = Object.keys(credentials).length > 0;
    const useBulkMode =
      !showMiners && !hasIndividualCredentials && bulkCredentials.username && bulkCredentials.password;

    if (useBulkMode) {
      // Bulk mode: Use all_devices selector with AUTHENTICATION_NEEDED filter
      // This allows authenticating all auth-needed miners without pagination limits
      const pairRequest = create(PairRequestSchema, {
        credentials: create(CredentialsSchema, {
          username: bulkCredentials.username,
          password: bulkCredentials.password,
        }),
        deviceSelector: create(DeviceSelectorSchema, {
          selectionType: {
            case: "allDevices",
            value: create(DeviceFilterSchema, {
              pairingStatus: [PairingStatus.AUTHENTICATION_NEEDED],
            }),
          },
        }),
      });

      pair({
        pairRequest,
        onSuccess: (failedDeviceIds) => {
          if (!isMountedRef.current) return;

          setAuthenticateLoading(false);
          setMinerErrors(failedDeviceIds);

          const successCount = totalMiners - failedDeviceIds.length;
          const allSucceeded = failedDeviceIds.length === 0;
          const allFailed = failedDeviceIds.length === totalMiners;

          if (allSucceeded) {
            pushToast({
              message: "All miners authenticated.",
              status: TOAST_STATUSES.success,
            });
            onClose();
          } else if (allFailed) {
            pushToast({
              message: "Authentication failed. Please check your credentials and try again.",
              status: TOAST_STATUSES.error,
            });
          } else {
            pushToast({
              message: `You authenticated ${successCount} of ${totalMiners} miners.`,
              status: TOAST_STATUSES.error,
            });
          }

          handleAuthenticationComplete(successCount);
        },
        onError: (error) => {
          if (!isMountedRef.current) return;

          console.error("Pairing error:", error);
          setAuthenticateLoading(false);
          pushToast({
            message: "Authentication failed. Please check your credentials and try again.",
            status: TOAST_STATUSES.error,
          });
        },
      });
      return;
    }

    // Individual mode: Group selected miners by their credentials
    // Uses include_devices selector with explicit device identifiers
    const credentialGroups = new Map<string, { creds: Credentials; deviceIds: string[] }>();

    selectedMiners.forEach((deviceId) => {
      const minerCreds = credentials[deviceId] || bulkCredentials;
      const key = `${minerCreds.username}|||${minerCreds.password}`;

      const existing = credentialGroups.get(key);
      if (existing) {
        existing.deviceIds.push(deviceId);
      } else {
        credentialGroups.set(key, {
          creds: minerCreds,
          deviceIds: [deviceId],
        });
      }
    });

    // Initialize or reset the completion tracker
    completionTrackerRef.current = {
      completed: 0,
      total: credentialGroups.size,
      failedMiners: [] as string[],
    };

    const handleRequestComplete = () => {
      completionTrackerRef.current.completed++;

      // Only process final results if all requests are complete
      if (completionTrackerRef.current.completed !== completionTrackerRef.current.total) return;

      // Check if component is still mounted before updating state
      if (!isMountedRef.current) return;

      setAuthenticateLoading(false);
      setMinerErrors(completionTrackerRef.current.failedMiners);

      const successCount = selectedMiners.length - completionTrackerRef.current.failedMiners.length;
      const allSucceeded = completionTrackerRef.current.failedMiners.length === 0;
      const allFailed = completionTrackerRef.current.failedMiners.length === selectedMiners.length;
      const loadedMinersCount = Object.keys(minersByIdentifier).length;
      const allMinersAuthenticated = allSucceeded && successCount === loadedMinersCount;

      if (allMinersAuthenticated) {
        pushToast({
          message: "All miners authenticated.",
          status: TOAST_STATUSES.success,
        });
        // Close modal after all miners in the list are successfully authenticated
        onClose();
      } else if (allSucceeded) {
        pushToast({
          message: `${successCount} ${successCount === 1 ? "miner" : "miners"} authenticated.`,
          status: TOAST_STATUSES.success,
        });
      } else if (allFailed) {
        pushToast({
          message: "Authentication failed. Please check your credentials and try again.",
          status: TOAST_STATUSES.error,
        });
      } else {
        pushToast({
          message: `You authenticated ${successCount} of ${selectedMiners.length} miners.`,
          status: TOAST_STATUSES.error,
        });
      }

      handleAuthenticationComplete(successCount);
    };

    // Make a pair request for each credential group using include_devices selector
    credentialGroups.forEach(({ creds, deviceIds }) => {
      const pairRequest = create(PairRequestSchema, {
        credentials: create(CredentialsSchema, {
          username: creds.username,
          password: creds.password,
        }),
        deviceSelector: create(DeviceSelectorSchema, {
          selectionType: {
            case: "includeDevices",
            value: create(DeviceIdentifierListSchema, {
              deviceIdentifiers: deviceIds,
            }),
          },
        }),
      });

      pair({
        pairRequest,
        onSuccess: (failedDeviceIds) => {
          // Safely aggregate failed device IDs
          completionTrackerRef.current.failedMiners.push(...failedDeviceIds);
          handleRequestComplete();
        },
        onError: (error) => {
          console.error("Pairing error:", error);
          // On error, mark all devices in this group as failed
          completionTrackerRef.current.failedMiners.push(...deviceIds);
          handleRequestComplete();
        },
      });
    });
  }, [
    bulkCredentials,
    credentials,
    selectedMiners,
    minersByIdentifier,
    showMiners,
    totalMiners,
    handleAuthenticationComplete,
    onClose,
    pair,
    setHasMissingCredentials,
    setAuthenticateLoading,
    setMinerErrors,
  ]);

  return (
    <Modal
      open={isVisible}
      divider={showMiners}
      onDismiss={onClose}
      buttons={[
        {
          variant: variants.textOnly,
          text: showMiners ? "Hide miner list" : "Show miners",
          onClick: () => {
            setCredentials({});
            setMinerErrors([]);
            setShowMiners((prev) => !prev);
          },
        },
        {
          variant: variants.primary,
          text: "Authenticate",
          dismissModalOnClick: false,
          loading: authenticateLoading,
          disabled: selectedMiners.length === 0,
          onClick: authenticateMiners,
        },
      ]}
      size={showMiners ? "large" : undefined}
      title="Authenticate miners"
      description={
        !showMiners
          ? "If miners use different credentials, we'll try each attempt until all miners are configured."
          : undefined
      }
    >
      {errorMessage !== null ? (
        <Callout
          className="mt-6"
          intent={intents.information}
          prefixIcon={<Alert className="text-text-critical" />}
          title={errorMessage}
          dismissible
          onDismiss={() => {
            setHasMissingCredentials(false);
            setMinerErrors([]);
          }}
        />
      ) : null}
      <div className="mt-6 rounded-2xl bg-surface-5 p-6 dark:bg-core-primary-5">
        <div className="flex w-full flex-col gap-4">
          <div>
            <div className="text-emphasis-300">Bulk authenticate</div>
            <div className="text-300">
              {totalMiners} {totalMiners === 1 ? "miner" : "miners"} remaining
            </div>
          </div>
          <Input
            id={ids.username}
            label="Miner username"
            initValue={bulkCredentials.username}
            disabled={authenticateLoading ? bulkCredentials.username !== "" : false}
            error={hasMissingCredentials && !bulkCredentials.username ? "Missing username" : undefined}
            onChange={handleBulkChange}
            autoFocus
          />
          <Input
            id={ids.password}
            label="Miner password"
            type="password"
            initValue={bulkCredentials.password}
            disabled={authenticateLoading ? bulkCredentials.password !== "" : false}
            error={hasMissingCredentials && !bulkCredentials.password ? "Missing password" : undefined}
            onChange={handleBulkChange}
          />
        </div>
      </div>
      {showMiners ? (
        <>
          <div className="mt-2">
            <List<UnauthenticatedMiner, UnauthenticatedMiner["deviceIdentifier"]>
              filters={filters}
              filterItem={filterByModel}
              onFilterChange={setActiveFilters}
              filterSize={sizes.compact}
              headerControls={<Switch label="Show passwords" checked={showPasswords} setChecked={setShowPasswords} />}
              activeCols={activeCols}
              colTitles={colTitles}
              colConfig={colConfig}
              items={minerItems}
              itemKey="deviceIdentifier"
              itemSelectable
              customSelectedItems={selectedMiners}
              customSetSelectedItems={setSelectedMiners}
              containerClassName="max-h-[50vh]"
              stickyBgColor="bg-surface-elevated-base"
            />
          </div>
          <ModalSelectAllFooter
            label={selectedMiners.length + " miners selected"}
            onSelectAll={() => setSelectedMiners(filteredMiners.map((miner) => miner.deviceIdentifier))}
            onSelectNone={() => setSelectedMiners([])}
          />
        </>
      ) : null}
    </Modal>
  );
};

export default AuthenticateMiners;
