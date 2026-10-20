import { useNavigate, useParams } from "react-router-dom";
import { buildContainerOverview } from "../buildContainerOverview";
import { buildEquipmentOverview } from "../buildEquipmentOverview";
import { tankPath } from "../buildTankOverview";
import ContainerOverviewLayout from "../components/ContainerOverviewLayout";
import ContainerPerformance from "../components/ContainerPerformance";
import ContainerActionsMenu from "../ContainerActionsMenu";
import { useContainerOverview } from "../hooks/useContainerOverview";
import { equipmentState, useEquipmentCommand } from "../hooks/useEquipmentCommand";
import { modulePath } from "../moduleDetail";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { entityScopeTarget, useSyncScopeToEntity } from "@/protoFleet/hooks/useSyncScopeToEntity";
import { useTemperatureUnit } from "@/protoFleet/store";
import Button, { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

export default function ContainerPage() {
  const { identifier = "" } = useParams<{ identifier: string }>();
  const navigate = useNavigate();
  const { data, loading, error, notFound, retry } = useContainerOverview(identifier);
  const { sites } = useSitesContext();
  const unit = useTemperatureUnit();
  const container = data?.container;
  const equipmentCommand = useEquipmentCommand(identifier, container, retry);
  const canWrite = container?.canControlEquipment === true;
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
  const equipment = buildEquipmentOverview(container, !!error, unit);
  return (
    <>
      <ContainerOverviewLayout
        actions={<ContainerActionsMenu container={container} onSiteChanged={retry} />}
        title={container.name}
        unlocatedHashboards={view.unlocatedHashboards}
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
        controls={
          equipment.controls.length
            ? equipment.controls.map((control) => ({
                ...control,
                on: !error && canWrite ? equipmentState(container, control.id) : control.on,
                disabled: equipmentCommand.busy || !!error,
                pending: equipmentCommand.pendingName === control.id,
              }))
            : undefined
        }
        onToggleControl={
          canWrite && !error ? (name, enabled) => void equipmentCommand.submit(name, enabled) : undefined
        }
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
        onSelectTank={(tankId) => navigate(tankPath(identifier, tankId))}
        onSelectModule={(id) => {
          const item = data.modules.find((module) => String(module.ref?.id) === id);
          const destination = item ? modulePath({ ...item, deviceIdentifier: identifier }) : undefined;
          if (destination) navigate(destination);
        }}
        plantControls={
          <div className="text-200 text-text-primary-70">
            {equipment.controlsNotice ? <p>{equipment.controlsNotice}</p> : null}
            {equipmentCommand.failed && equipmentCommand.message ? (
              <p role="alert">{equipmentCommand.message}</p>
            ) : null}
          </div>
        }
        performance={<ContainerPerformance key={identifier} identifier={identifier} siteId={container.site?.id} />}
      />
    </>
  );
}
