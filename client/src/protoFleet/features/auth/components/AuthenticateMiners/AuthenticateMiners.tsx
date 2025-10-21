import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import useFleet from "@/protoFleet/api/useFleet";
import { ids } from "@/protoFleet/features/auth/components/AuthenticateMiners/constants";
import {
  Credentials,
  UnauthenticatedMiner,
} from "@/protoFleet/features/auth/components/AuthenticateMiners/types";
import { useFleetMiners } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button/constants";
import Callout, { intents } from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import {
  ActiveFilters,
  DropdownFilterItem,
} from "@/shared/components/List/Filters/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import { sizes as modalSizes } from "@/shared/components/Modal/constants";
import Switch from "@/shared/components/Switch";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";

const activeCols = [
  "model",
  "macAddress",
  "username",
  "password",
] as (keyof UnauthenticatedMiner)[];

const colTitles = {
  model: "Model",
  macAddress: "MAC address",
  username: "Username",
  password: "Password",
} as {
  [key in (typeof activeCols)[number]]: string;
};

// TODO remove when API is available
const mockModels = [
  "Proto Rig",
  "Antminer S19",
  "Whatsminer M30S",
  "AvalonMiner 1246",
  "Bitmain Antminer L7",
];

type AuthenticateMinersProps = {
  onClose: () => void;
};

const AuthenticateMiners = ({ onClose }: AuthenticateMinersProps) => {
  const [bulkCredentials, setBulkCredentials] = useState<Credentials>({
    username: "",
    password: "",
  });
  // stores credentials for each miner, keyed by deviceIdentifier
  const [credentials, setCredentials] = useState<
    Record<UnauthenticatedMiner["deviceIdentifier"], Credentials>
  >({});
  const [hasMissingCredentials, setHasMissingCredentials] = useState(false);
  // stores ids of miners that have errors
  const [minerErrors, setMinerErrors] = useState<
    UnauthenticatedMiner["deviceIdentifier"][]
  >([]);
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

  // TODO get unauthenticated miners instead of all miners
  const { minerIds } = useFleet({ pageSize: 100 });
  const minerItems = useFleetMiners().map((miner, index) => ({
    ...miner,
    // add random mock model
    model: mockModels[index % mockModels.length],
    username: "",
    password: "",
  }));

  const [selectedMiners, setSelectedMiners] = useState<string[]>([]);
  // select all miners by default
  useEffect(() => {
    setSelectedMiners(minerItems.map((miner) => miner.deviceIdentifier));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [minerIds]);

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

  const filterItem = useCallback(
    (item: UnauthenticatedMiner, filters: ActiveFilters) => {
      if (
        filters.dropdownFilters &&
        filters.dropdownFilters["model"] &&
        !filters.dropdownFilters["model"].includes("all")
      ) {
        if (!filters.dropdownFilters["model"].includes(item.model)) {
          return false;
        }
      }
      return true;
    },
    [],
  );

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
            className="!h-10"
            label="Username"
            initValue={bulkCredentials.username}
            hideLabelOnFocus
            disabled={
              authenticateLoading &&
              (bulkCredentials.username !== "" ||
                (credentials[item.deviceIdentifier] !== undefined &&
                  credentials[item.deviceIdentifier].username !== ""))
            }
            error={
              minerErrors.find((id) => id === item.deviceIdentifier) !==
              undefined
            }
            onChange={handleMinerChange.bind(
              this,
              item.deviceIdentifier,
              ids.username,
            )}
          />
        ),
        width: "w-70 !py-3",
      },
      password: {
        component: (item: UnauthenticatedMiner) => (
          <Input
            id={item.deviceIdentifier + "_password"}
            className="!h-10"
            label="Password"
            type={showPasswords ? "text" : "password"}
            initValue={bulkCredentials.password}
            hideLabelOnFocus
            disabled={
              authenticateLoading &&
              (bulkCredentials.password !== "" ||
                (credentials[item.deviceIdentifier] !== undefined &&
                  credentials[item.deviceIdentifier].password !== ""))
            }
            error={
              minerErrors.find((id) => id === item.deviceIdentifier) !==
              undefined
            }
            onChange={handleMinerChange.bind(
              this,
              item.deviceIdentifier,
              ids.password,
            )}
          />
        ),
        width: "w-70 !py-3",
      },
    };
  }, [
    handleMinerChange,
    bulkCredentials,
    showPasswords,
    authenticateLoading,
    minerErrors,
    credentials,
  ]);

  const authenticateMiners = useCallback(() => {
    if (
      (bulkCredentials.username === "" || bulkCredentials.password === "") &&
      Object.entries(credentials).length === 0
    ) {
      setHasMissingCredentials(true);
      return;
    }

    // TODO call API to authenticate miners with the provided credentials
    // TODO submit credentials only for selected miners
    // TODO update the number of miners left to authenticate
    // TODO update list of unauthenticated miners
    setAuthenticateLoading(true);
    setTimeout(() => {
      setAuthenticateLoading(false);
      pushToast({
        message: "You authenticated 1 of 17 miners",
        status: TOAST_STATUSES.success,
      });

      // mock error for the second miner
      setMinerErrors([minerItems[1].deviceIdentifier]);
    }, 1500);
  }, [bulkCredentials, credentials, minerItems]);

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
      <div className="mt-6 rounded-2xl bg-surface-5 p-6">
        <div className="flex w-full flex-wrap gap-4">
          <div
            className={clsx({
              "flex-1 content-center": showMiners,
              "flex w-full gap-2": !showMiners,
            })}
          >
            <div className="text-emphasis-300">Bulk authenticate</div>
            <div className="text-300">17 miners remaining</div>
          </div>
          <div className="flex-1">
            <Input
              id={ids.username}
              label="Miner username"
              initValue={bulkCredentials.username}
              disabled={authenticateLoading && bulkCredentials.username !== ""}
              error={
                hasMissingCredentials && !bulkCredentials.username
                  ? "Missing username"
                  : undefined
              }
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
              error={
                hasMissingCredentials && !bulkCredentials.password
                  ? "Missing password"
                  : undefined
              }
              onChange={handleBulkChange}
            />
          </div>
        </div>
      </div>
      {showMiners && (
        <>
          <div className="mt-2">
            <List<
              UnauthenticatedMiner,
              UnauthenticatedMiner["deviceIdentifier"]
            >
              filters={[modelFilter]}
              filterItem={filterItem}
              filterSize={sizes.compact}
              headerControls={
                <Switch
                  label="Show passwords"
                  checked={showPasswords}
                  setChecked={setShowPasswords}
                />
              }
              activeCols={activeCols}
              colTitles={colTitles}
              colConfig={colConfig}
              items={minerItems}
              itemKey="deviceIdentifier"
              itemSelectable
              customSelectedItems={selectedMiners}
              customSetSelectedItems={setSelectedMiners}
              containerClassName="max-h-[50vh]"
            />
          </div>
          <ModalSelectAllFooter
            label={selectedMiners.length + " miners selected"}
            onSelectAll={() =>
              setSelectedMiners(
                minerItems.map((miner) => miner.deviceIdentifier),
              )
            }
            onSelectNone={() => setSelectedMiners([])}
          />
        </>
      )}
    </Modal>
  );
};

export default AuthenticateMiners;
