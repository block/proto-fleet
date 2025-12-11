import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { create } from "@bufbuild/protobuf";
import { CredentialsSchema, PairRequestSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { ids } from "@/protoFleet/features/auth/components/AuthenticateMiners/constants";
import { Credentials, UnauthenticatedMiner } from "@/protoFleet/features/auth/components/AuthenticateMiners/types";
import { useFleetStore } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button/constants";
import Callout, { intents } from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import { ActiveFilters, DropdownFilterItem } from "@/shared/components/List/Filters/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import { sizes as modalSizes } from "@/shared/components/Modal/constants";
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
  onClose: () => void;
  onSuccess?: () => void;
};

const AuthenticateMiners = ({ onClose, onSuccess }: AuthenticateMinersProps) => {
  // Component fetches its own data
  const { miners: minersByIdentifier, refetch: refetchAuthNeededMiners } = useAuthNeededMiners();
  const { pair } = useMinerPairing();
  const { refetch: refetchOnboardingStatus } = useOnboardedStatus();

  // Track if component is mounted to prevent state updates after unmount
  const isMountedRef = useRef(true);

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
      setBulkCredentials({ ...bulkCredentials, [id]: value.trim() });
    },
    [bulkCredentials],
  );

  const handleMinerChange = useCallback(
    (deviceIdentifier: string, key: string, value: string) => {
      const newValue = { ...credentials };
      newValue[deviceIdentifier] = {
        ...(credentials[deviceIdentifier] || {}),
        [key]: value.trim(),
      };
      setCredentials(newValue);
    },
    [credentials],
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
  // select all miners by default
  useEffect(() => {
    setSelectedMiners(Object.keys(minersByIdentifier));
  }, [minersByIdentifier]);

  const models = useMemo(() => {
    return Array.from(new Set(minerItems.map((miner) => miner.model)));
  }, [minerItems]);

  const modelFilter = useMemo(() => {
    const options = models.map((model) => ({
      id: model,
      label: model,
    }));

    return {
      type: "dropdown",
      title: "Model",
      value: "model",
      options: [...options],
      defaultOptionIds: [...options.map((o) => o.id)],
    } as DropdownFilterItem;
  }, [models]);

  const filterItem = useCallback((item: UnauthenticatedMiner, filters: ActiveFilters) => {
    const modelFilters = filters.dropdownFilters?.["model"];

    // If no model filter is applied (empty array or undefined), show all items
    if (!modelFilters || modelFilters.length === 0) {
      return true;
    }

    // If model filters are applied, only show items that match
    if (!modelFilters.includes(item.model)) {
      return false;
    }

    return true;
  }, []);

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
              authenticateLoading &&
              (bulkCredentials.username !== "" ||
                (credentials[item.deviceIdentifier] !== undefined &&
                  credentials[item.deviceIdentifier].username !== ""))
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
              authenticateLoading &&
              (bulkCredentials.password !== "" ||
                (credentials[item.deviceIdentifier] !== undefined &&
                  credentials[item.deviceIdentifier].password !== ""))
            }
            error={minerErrors.find((id) => id === item.deviceIdentifier) !== undefined}
            onChange={handleMinerChange.bind(this, item.deviceIdentifier, ids.password)}
          />
        ),
        width: "w-70 !py-3",
      },
    };
  }, [handleMinerChange, bulkCredentials, showPasswords, authenticateLoading, minerErrors, credentials]);

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

    // Group selected miners by their credentials
    // If a miner has individual credentials, use those; otherwise use bulk credentials
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

    const completionTracker = {
      completed: 0,
      total: credentialGroups.size,
      failedMiners: [] as string[],
    };

    const handleRequestComplete = () => {
      completionTracker.completed++;

      // Only process final results if all requests are complete
      if (completionTracker.completed !== completionTracker.total) return;

      // Check if component is still mounted before updating state
      if (!isMountedRef.current) return;

      setAuthenticateLoading(false);
      setMinerErrors(completionTracker.failedMiners);

      const successCount = selectedMiners.length - completionTracker.failedMiners.length;
      const allSucceeded = completionTracker.failedMiners.length === 0;
      const allFailed = completionTracker.failedMiners.length === selectedMiners.length;
      const totalMiners = Object.keys(minersByIdentifier).length;
      const allMinersAuthenticated = allSucceeded && successCount === totalMiners;

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

      refetchOnboardingStatus();
      // Refetch global fleet state if callback is available
      useFleetStore.getState().fleet.refetchMiners?.();
      refetchAuthNeededMiners();
      // Call parent's success handler if at least one miner was authenticated
      if (successCount > 0) {
        onSuccess?.();
      }
    };

    // Make a pair request for each credential group
    credentialGroups.forEach(({ creds, deviceIds }) => {
      const pairRequest = create(PairRequestSchema, {
        deviceIdentifiers: deviceIds,
        credentials: create(CredentialsSchema, {
          username: creds.username,
          password: creds.password,
        }),
      });

      pair({
        pairRequest,
        onSuccess: (failedDeviceIds) => {
          // Safely aggregate failed device IDs
          completionTracker.failedMiners.push(...failedDeviceIds);
          handleRequestComplete();
        },
        onError: (error) => {
          console.error("Pairing error:", error);
          // On error, mark all devices in this group as failed
          completionTracker.failedMiners.push(...deviceIds);
          handleRequestComplete();
        },
      });
    });
  }, [
    bulkCredentials,
    credentials,
    selectedMiners,
    minersByIdentifier,
    refetchOnboardingStatus,
    refetchAuthNeededMiners,
    onClose,
    onSuccess,
    pair,
  ]);

  return (
    <Modal
      divider={showMiners}
      onDismiss={onClose}
      show
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
          onClick: authenticateMiners,
        },
      ]}
      buttonSize={sizes.base}
      size={showMiners ? modalSizes.extraLarge : modalSizes.large}
      title={showMiners ? "Authenticate miners" : undefined}
    >
      {!showMiners && (
        <Header
          title="Authenticate miners"
          titleSize="text-heading-300"
          subtitle="If miners use different credentials, we'll try each attempt until all miners are configured."
          subtitleSize="text-300"
          className="bg-surface-elevated-base"
        />
      )}
      {errorMessage !== null && (
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
      )}
      <div className="mt-6 rounded-2xl bg-surface-5 p-6 dark:bg-core-primary-5">
        <div className="flex w-full flex-wrap gap-4">
          <div
            className={clsx({
              "flex-1 content-center": showMiners,
              "flex w-full gap-2": !showMiners,
            })}
          >
            <div className="text-emphasis-300">Bulk authenticate</div>
            <div className="text-300">
              {minerItems.length} {minerItems.length === 1 ? "miner" : "miners"} remaining
            </div>
          </div>
          <div className="flex-1">
            <Input
              id={ids.username}
              label="Miner username"
              initValue={bulkCredentials.username}
              disabled={authenticateLoading && bulkCredentials.username !== ""}
              error={hasMissingCredentials && !bulkCredentials.username ? "Missing username" : undefined}
              onChange={handleBulkChange}
            />
          </div>
          <div className="flex-1">
            <Input
              id={ids.password}
              label="Miner password"
              type="password"
              initValue={bulkCredentials.password}
              disabled={authenticateLoading && bulkCredentials.password !== ""}
              error={hasMissingCredentials && !bulkCredentials.password ? "Missing password" : undefined}
              onChange={handleBulkChange}
            />
          </div>
        </div>
      </div>
      {showMiners && (
        <>
          <div className="mt-2">
            <List<UnauthenticatedMiner, UnauthenticatedMiner["deviceIdentifier"]>
              filters={[modelFilter]}
              filterItem={filterItem}
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
            onSelectAll={() => setSelectedMiners(minerItems.map((miner) => miner.deviceIdentifier))}
            onSelectNone={() => setSelectedMiners([])}
          />
        </>
      )}
    </Modal>
  );
};

export default AuthenticateMiners;
