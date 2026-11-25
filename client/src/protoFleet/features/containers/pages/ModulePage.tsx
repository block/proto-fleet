import { Link, useParams } from "react-router-dom";
import { tankPath } from "../buildTankOverview";
import ModuleDetails from "../components/ModuleDetails";
import { useContainerOverview } from "../hooks/useContainerOverview";
import { resolveModule } from "../moduleDetail";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { entityScopeTarget, useSyncScopeToEntity } from "@/protoFleet/hooks/useSyncScopeToEntity";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { Alert } from "@/shared/assets/icons";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";

export default function ModulePage() {
  const { identifier = "", tankId = "", moduleId = "" } = useParams();
  const { data, loading, error, retry } = useContainerOverview(identifier);
  const { sites } = useSitesContext();
  const target = data ? entityScopeTarget(data.container.site?.id, sites) : undefined;
  useSyncScopeToEntity(target);
  const detail = data ? resolveModule(data, tankId, moduleId) : undefined;
  if (loading)
    return (
      <p role="status" className="p-8">
        Loading module…
      </p>
    );
  if (!data || !detail)
    return (
      <div className="flex flex-col gap-4 p-8">
        <Header title={<h1>Module unavailable</h1>} titleSize="text-heading-300" />
        <p role="alert">
          {error ||
            "This module may have been replaced or removed, may belong to another tank, or you may no longer have access."}
        </p>
        <Button variant="secondary" className="w-fit" text="Retry" onClick={retry} />
        <Link to={tankPath(identifier, tankId)}>Back to tank</Link>
      </div>
    );
  const { container } = data;
  const { module, tank } = detail;
  const path = tankPath(identifier, tankId);
  return (
    <div className="flex flex-col gap-8 p-6 laptop:p-10" data-testid="module-detail">
      {error ? (
        <div role="alert">
          <Callout
            intent="danger"
            prefixIcon={<Alert />}
            title="Could not refresh module"
            subtitle={error}
            buttonText="Retry"
            buttonOnClick={retry}
          />
        </div>
      ) : null}
      <ModuleDetails
        key={`${identifier}:${tankId}:${moduleId}:${container.site?.id}`}
        header={
          <div className="flex flex-col gap-3">
            <Breadcrumb
              segments={[
                { label: "Buildings", to: scopedPath("/fleet/buildings", target ?? { kind: "all" }) },
                ...(container.site ? [{ label: container.site.label, to: `/sites/${container.site.id}` }] : []),
                { label: container.name, to: `/containers/${encodeURIComponent(identifier)}` },
                { label: `Tank ${tank.position}`, to: path },
                { label: `Module ${module.position}` },
              ]}
            />
            <div className="flex flex-wrap items-center justify-between gap-3">
              <Header
                className="min-w-0 flex-1"
                title={<h1>{module.name}</h1>}
                titleSize="text-heading-300 break-words"
              />
            </div>
          </div>
        }
        module={module}
        unavailable={!!error || container.stale}
        enabled={!error}
        refresh={retry}
      />
    </div>
  );
}
