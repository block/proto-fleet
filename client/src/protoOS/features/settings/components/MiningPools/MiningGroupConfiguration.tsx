import { useEffect, useState } from "react";
import MiningGroupOperations from "./MiningGroupOperations";
import type {
  ContainerPoolConfiguration,
  MiningGroupPoolSettings,
  PoolBoardAssignment,
} from "@/protoOS/api/adapters/poolConfiguration";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import Button from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

type PoolGroupReader = Pick<MinerAdapter, "getMiningGroupPoolSettings">;

interface GroupProps {
  adapter: PoolGroupReader;
  group: ContainerPoolConfiguration["groups"][number];
}

const BoardAssignments = ({ boards }: { boards: PoolBoardAssignment[] }) => (
  <ul className="break-words">
    {boards.map((board) => (
      <li key={board.mac}>
        <Row>
          <div>{board.location ?? "Location unavailable"}</div>
          <div className="text-200 text-text-primary-70">{board.mac}</div>
        </Row>
      </li>
    ))}
  </ul>
);

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
    return <p role="alert">Could not read this group's pools. Reload pools to read the current configuration.</p>;
  if (!pools) return <ProgressCircular indeterminate />;
  if (!pools.length) return <p>No pools configured for this group.</p>;
  return (
    <ul>
      {pools.map((pool, index) => (
        <li key={`${pool.id}:${index}`}>
          <Row>
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
          text={`${detailsAction} details`}
          ariaLabel={`${detailsAction} ${group.name} details`}
          ariaExpanded={open}
          onClick={() => setOpen(!open)}
        />
      }
    >
      <p className="text-200 text-text-primary-70">
        {group.boards.length} {group.boards.length === 1 ? "hashboard" : "hashboards"}
      </p>
      {open ? (
        <>
          <h4 className="text-emphasis-300">Configured pools</h4>
          {group.revision && adapter.getMiningGroupPoolSettings ? (
            <GroupPools adapter={adapter} group={group} />
          ) : (
            <p role="alert">Group details are unavailable. Reload pools.</p>
          )}
          <h4 className="text-emphasis-300">Assigned hashboards</h4>
          {group.boards.length ? <BoardAssignments boards={group.boards} /> : <p>No hashboards assigned.</p>}
        </>
      ) : null}
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
      <p>
        Mining groups are enabled. Container-wide pool editing is unavailable here. Manage hosted assignments from
        Fleet.
      </p>
      <p className="text-200 text-text-primary-70">
        These are controller-reported assignments and configured usernames. They do not confirm that each board has
        applied the settings.
      </p>
      {configuration.suspended ? <p role="status">Mining groups are suspended.</p> : null}
      {!configuration.locationsAvailable ? (
        <p role="status">Board locations are unavailable. Assignments are shown by MAC address.</p>
      ) : null}
      <MiningGroupOperations operations={configuration.pendingOperations} />
      {configuration.inventoryIssues.length ? (
        <section aria-label="Assignment issues">
          <h3 className="text-emphasis-300">Assignment issues</h3>
          {configuration.inventoryIssues.map((issue, index) => (
            <Row key={index}>
              <p className="break-words">
                {issue.mac || configuration.groups.find((group) => group.id === issue.groupId)?.name || "Container"}:{" "}
                {issue.reason}
              </p>
            </Row>
          ))}
        </section>
      ) : null}
      {configuration.groups.map((group) => (
        <Group key={`${group.id}:${group.revision}`} group={group} adapter={adapter} />
      ))}
      {!configuration.groups.length ? <p>No mining groups reported.</p> : null}
      {configuration.unassignedBoards.length ? (
        <details>
          <summary className="cursor-pointer text-emphasis-300">
            No reported group assignment ({configuration.unassignedBoards.length})
          </summary>
          <BoardAssignments boards={configuration.unassignedBoards} />
        </details>
      ) : null}
    </div>
  );
}
