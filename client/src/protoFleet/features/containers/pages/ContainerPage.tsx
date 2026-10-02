import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { buildContainerOverview } from "../buildContainerOverview";
import { buildEquipmentOverview } from "../buildEquipmentOverview";
import ContainerOverviewLayout from "../components/ContainerOverviewLayout";
import ContainerPerformance from "../components/ContainerPerformance";
import { useContainerOverview } from "../hooks/useContainerOverview";
import { ModuleHistoryDialog } from "../ModuleHistoryDialog";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { entityScopeTarget, useSyncScopeToEntity } from "@/protoFleet/hooks/useSyncScopeToEntity";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useTemperatureUnit } from "@/protoFleet/store";
import Button, { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

export default function ContainerPage() {
  const { identifier = "" } = useParams<{ identifier: string }>();
  const navigate = useNavigate();
  const { data, loading, error, notFound, retry } = useContainerOverview(identifier);
  const { sites } = useSitesContext();
  const unit = useTemperatureUnit();
  const [selected, setSelected] = useState<{ container: string; module: string }>();
  const container = data?.container;
  const target = container ? entityScopeTarget(container.site?.id, sites) : undefined;
  useSyncScopeToEntity(target);
  if (loading)
    return (
      <p role="status" className="p-8">
        Loading container…
      </p>
    );
  if (!data || !container)
    return (
      <div className="flex flex-col gap-4 p-8">
        <Header title={notFound ? "Container not found" : "Couldn't load container"} titleSize="text-heading-300" />
        <p role="alert">
          {notFound
            ? "This container may have been removed or you may no longer have access."
            : error || "No container selected."}
        </p>
        <Button variant={variants.secondary} text="Retry" onClick={retry} />
      </div>
    );
  const view = buildContainerOverview(data, unit, !!error);
  const equipment = buildEquipmentOverview(container, !!error);
  const module =
    selected?.container === identifier
      ? data.modules.find((item) => item.ref?.id.toString() === selected.module)
      : undefined;
  const moduleList = (tankId: string) => scopedPath(`/fleet/miners?tank=${tankId}`, target ?? { kind: "all" });
  return (
    <>
      <ContainerOverviewLayout
        title={container.name}
        breadcrumb={
          container.site
            ? [
                { label: "Sites", to: "/fleet/sites" },
                { label: container.site.label, to: `/sites/${container.site.id}` },
                { label: container.name },
              ]
            : [{ label: "Buildings", to: "/fleet/buildings" }, { label: container.name }]
        }
        kpis={view.kpis}
        tanks={view.tanks}
        fans={equipment.fans}
        controls={equipment.controls.length ? equipment.controls : undefined}
        notice={
          view.stale || error ? (
            <div className="flex flex-wrap items-center justify-between gap-3 text-200 text-text-primary-70">
              {view.stale ? <p role="status">Readings unavailable or stale.</p> : null}
              {error ? (
                <>
                  <p role="alert">Could not refresh container: {error}</p>
                  <Button variant={variants.secondary} text="Retry" onClick={retry} />
                </>
              ) : null}
            </div>
          ) : undefined
        }
        onSelectTank={(tankId) => navigate(moduleList(tankId))}
        onSelectModule={(module) => setSelected({ container: identifier, module })}
        plantControls={
          equipment.controlsNotice ? (
            <p className="text-200 text-text-primary-70">{equipment.controlsNotice}</p>
          ) : undefined
        }
        performance={<ContainerPerformance key={identifier} identifier={identifier} siteId={container.site?.id} />}
      />
      {module ? (
        <ModuleHistoryDialog
          key={`${identifier}:${module.ref?.id}`}
          item={module}
          onDismiss={() => setSelected(undefined)}
        />
      ) : null}
    </>
  );
}
