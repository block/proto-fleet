import { useEffect, useState } from "react";
import MiningGroupOperations from "./MiningGroupOperations";
import PoolHashboards from "./PoolHashboards";
import type { ContainerPoolConfiguration, MiningGroupPoolSettings } from "@/protoOS/api/adapters/poolConfiguration";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import { Alert, ChevronDown } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Card, { cardType } from "@/shared/components/Card";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

type PoolGroupReader = Pick<MinerAdapter, "getMiningGroupPoolSettings">;

interface GroupProps {
  adapter: PoolGroupReader;
  group: ContainerPoolConfiguration["groups"][number];
}

function GroupPools({ adapter, group }: GroupProps) {
  const [pools, setPools] = useState<MiningGroupPoolSettings[]>();
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    void adapter.getMiningGroupPoolSettings!(group.id, group.revision, { signal: controller.signal }).then(
      (value) => {
        if (!controller.signal.aborted) setPools(value);
      },
      () => {
        if (!controller.signal.aborted) setFailed(true);
      },
    );
    return () => controller.abort();
  }, [adapter, group.id, group.revision]);

  if (failed)
    return (
      <div role="alert">
        <Callout
          intent={intents.danger}
          prefixIcon={<Alert />}
          title="Could not load group pools."
          subtitle="Reload pools to try again."
        />
      </div>
    );
  if (!pools) return <ProgressCircular indeterminate />;
  if (!pools.length) return <p className="text-300 text-text-primary-70">No pools configured for this group.</p>;
  return (
    <ul>
      {pools.map((pool, index) => (
        <li key={`${pool.id}:${index}`}>
          <Row divider={index < pools.length - 1} className={index === 0 ? "pt-0" : undefined}>
            <div className="text-emphasis-300 break-words">{pool.name || pool.url || "Unnamed pool"}</div>
            <dl className="space-y-1 text-200 break-words">
              <div>
                <dt className="inline text-text-primary-70">URL: </dt>
                <dd className="inline">{pool.url || "Not reported"}</dd>
              </div>
              <div>
                <dt className="inline text-text-primary-70">Username: </dt>
                <dd className="inline">{pool.username || "Not reported"}</dd>
              </div>
              <div>
                <dt className="inline text-text-primary-70">Configuration: </dt>
                <dd className="inline">
                  {pool.enabled ? "Enabled" : "Disabled"} · Priority {pool.priority + 1}
                  {pool.shareWeight !== undefined && pool.shareWeight > 0 ? ` · Weight ${pool.shareWeight}` : ""}
                </dd>
              </div>
            </dl>
          </Row>
        </li>
      ))}
    </ul>
  );
}

function Group({ adapter, group }: GroupProps) {
  const [open, setOpen] = useState(false);
  const detailsAction = open ? "Hide" : "View";
  return (
    <Card
      title={<h3 className="text-emphasis-300 break-words">{group.name}</h3>}
      type={cardType.default}
      headerTone="neutral"
      bodyClassName="space-y-3 px-4 pb-4"
      headerAction={
        <Button
          variant="textOnly"
          textColor="text-text-primary-70"
          className="p-2"
          prefixIcon={<ChevronDown width="w-4" className={open ? undefined : "-rotate-90"} />}
          ariaLabel={`${detailsAction} ${group.name} pools`}
          ariaExpanded={open}
          onClick={() => setOpen(!open)}
        />
      }
    >
      {open ? (
        <section aria-label="Configured pools">
          {group.revision && adapter.getMiningGroupPoolSettings ? (
            <GroupPools adapter={adapter} group={group} />
          ) : (
            <div role="alert">
              <Callout
                intent={intents.danger}
                prefixIcon={<Alert />}
                title="Group details are unavailable."
                subtitle="Reload pools to try again."
              />
            </div>
          )}
        </section>
      ) : null}
      <PoolHashboards boards={group.boards} title={`${group.name} hashboards`} countLabel="Assigned hashboards" />
    </Card>
  );
}

export default function MiningGroupConfiguration({
  configuration,
  adapter,
}: {
  configuration: ContainerPoolConfiguration;
  adapter: PoolGroupReader;
}) {
  return (
    <div className="space-y-6">
      {configuration.suspended ? (
        <div role="status">
          <Callout intent={intents.warning} prefixIcon={<Alert />} title="Container mining is paused." />
        </div>
      ) : null}
      {!configuration.locationsAvailable ? (
        <div role="status">
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title="Board locations are unavailable."
            subtitle="Boards are listed by MAC address."
          />
        </div>
      ) : null}
      <MiningGroupOperations operations={configuration.pendingOperations} />
      {configuration.inventoryIssues.length ? (
        <section aria-label="Assignment issues" className="space-y-3">
          <h3 className="text-emphasis-300">Assignment issues</h3>
          {configuration.inventoryIssues.map((issue, index) => (
            <div key={index} role="alert">
              <Callout
                intent={intents.warning}
                prefixIcon={<Alert />}
                title={issue.reason}
                subtitle={
                  issue.mac || configuration.groups.find((group) => group.id === issue.groupId)?.name || "Container"
                }
                className="break-words"
              />
            </div>
          ))}
        </section>
      ) : null}
      {configuration.groups.map((group) => (
        <Group key={`${group.id}:${group.revision}`} group={group} adapter={adapter} />
      ))}
      {!configuration.groups.length ? <p>No mining groups reported.</p> : null}
      {configuration.unassignedBoards.length ? (
        <section aria-label="No reported group assignment" className="space-y-3">
          <h3 className="text-emphasis-300">No reported group assignment</h3>
          <PoolHashboards
            boards={configuration.unassignedBoards}
            title="Hashboards with no reported group assignment"
          />
        </section>
      ) : null}
    </div>
  );
}
