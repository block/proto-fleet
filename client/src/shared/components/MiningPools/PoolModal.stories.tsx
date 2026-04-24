import { useState } from "react";
import { action } from "storybook/actions";
import { emptyPoolInfo } from "./constants";
import PoolModal from "./PoolModal";
import type { PoolInfo } from "./types";

export default {
  title: "Shared/MiningPools/PoolModal",
  component: PoolModal,
};

export const AddPool = () => {
  const [open, setOpen] = useState(true);
  const [pools, setPools] = useState<PoolInfo[]>([emptyPoolInfo]);

  return (
    <>
      {!open ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <PoolModal
        open={open}
        pools={pools}
        poolIndex={0}
        onChangePools={(newPools) => {
          action("onChangePools")(newPools);
          setPools(newPools);
        }}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        isTestingConnection={false}
        testConnection={(args) => {
          action("testConnection")(args.poolInfo);
          args.onSuccess?.();
          args.onFinally?.();
        }}
        mode="add"
      />
    </>
  );
};

export const EditPool = () => {
  const [open, setOpen] = useState(true);
  const [pools, setPools] = useState<PoolInfo[]>([
    {
      name: "SlushPool",
      url: "stratum+tcp://stratum.slushpool.com:3333",
      username: "worker1",
      password: "",
      priority: 0,
    },
  ]);

  return (
    <>
      {!open ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <PoolModal
        open={open}
        pools={pools}
        poolIndex={0}
        onChangePools={(newPools) => {
          action("onChangePools")(newPools);
          setPools(newPools);
        }}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        isTestingConnection={false}
        testConnection={(args) => {
          action("testConnection")(args.poolInfo);
          args.onSuccess?.();
          args.onFinally?.();
        }}
        mode="edit"
        onDelete={() => action("onDelete")()}
      />
    </>
  );
};
