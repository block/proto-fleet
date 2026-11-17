import type { ContainerPoolConfiguration } from "@/protoOS/api/adapters/poolConfiguration";
import { Alert } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";
import Row from "@/shared/components/Row";

export default function MiningGroupOperations({
  operations,
}: {
  operations: ContainerPoolConfiguration["pendingOperations"];
}) {
  if (!operations.length) return null;
  return (
    <section aria-label="Group changes" className="mb-6">
      <h3 className="text-emphasis-300">Group changes</h3>
      {operations.map((operation, index) => (
        <Row key={`${operation.id}:${index}`}>
          <p>
            {operation.kind} · {operation.phase}
          </p>
          {operation.error ? (
            <div role="alert" className="my-2">
              <Callout intent={intents.danger} prefixIcon={<Alert />} title={operation.error} />
            </div>
          ) : null}
          {operation.boards.length ? (
            <p className="text-200 break-words text-text-primary-70">{operation.boards.join(", ")}</p>
          ) : null}
        </Row>
      ))}
    </section>
  );
}
