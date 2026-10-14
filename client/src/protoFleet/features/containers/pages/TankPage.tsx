import { Link, useNavigate, useParams } from "react-router-dom";
import { buildTankOverview, tankPath } from "../buildTankOverview";
import SubtankDetail from "../components/SubtankDetail";
import TankPerformance from "../components/TankPerformance";
import { useContainerOverview } from "../hooks/useContainerOverview";
import MiningActionsMenu from "../MiningActionsMenu";
import { ModuleHistoryDialog } from "../ModuleHistoryDialog";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { entityScopeTarget, useSyncScopeToEntity } from "@/protoFleet/hooks/useSyncScopeToEntity";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useTemperatureUnit } from "@/protoFleet/store";
import Button, { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

export default function TankPage() {
  const { identifier = "", tankId = "", moduleId } = useParams();
  const navigate = useNavigate();
  const { data, loading, error, notFound, retry } = useContainerOverview(identifier);
  const { sites } = useSitesContext();
  const unit = useTemperatureUnit();
  const target = data ? entityScopeTarget(data.container.site?.id, sites) : undefined;
  useSyncScopeToEntity(target);
  const view = data ? buildTankOverview(data, tankId, unit, !!error) : undefined;

  if (loading)
    return (
      <p role="status" className="p-8">
        Loading tank…
      </p>
    );
  if (!data || !view)
    return (
      <div className="flex flex-col gap-4 p-8">
        <Header
          title={<h1>{notFound || data ? "Tank not found" : "Couldn't load tank"}</h1>}
          titleSize="text-heading-300"
        />
        <p role="alert">{error || "This tank may have been removed or you may no longer have access."}</p>
        <Button variant={variants.secondary} text="Retry" onClick={retry} />
        <Link to={`/containers/${encodeURIComponent(identifier)}`}>Back to container</Link>
      </div>
    );

  const { container } = data;
  const path = tankPath(identifier, tankId);
  const module = view.modules.find((item) => item.ref?.id.toString() === moduleId);
  const containerPath = `/containers/${encodeURIComponent(identifier)}`;
  return (
    <>
      <SubtankDetail
        title={view.title}
        actions={
          <MiningActionsMenu kind="tank" key={`${identifier}:${tankId}`} items={[view.tank]} onRefresh={retry} />
        }
        breadcrumb={[
          { label: "Buildings", to: scopedPath("/fleet/buildings", target ?? { kind: "all" }) },
          ...(container.site ? [{ label: container.site.label, to: `/sites/${container.site.id}` }] : []),
          { label: container.name, to: containerPath },
          { label: view.title, to: path },
        ]}
        kpis={view.kpis}
        rows={view.card.rows}
        cols={view.card.cols}
        modules={view.card.modules}
        slots={view.card.slots}
        on={undefined}
        onSelectModule={(id) => navigate(`${path}/modules/${id}`)}
        onViewMiners={() => navigate(scopedPath(`/fleet/miners?tank=${tankId}`, target ?? { kind: "all" }))}
        notice={
          view.stale || error || (moduleId && !module) ? (
            <div className="flex flex-col gap-2 text-200 text-text-primary-70">
              {view.stale ? <p role="status">Readings unavailable or stale.</p> : null}
              {error ? (
                <>
                  <p role="alert">Could not refresh tank: {error}</p>
                  <Button variant={variants.secondary} text="Retry" onClick={retry} />
                </>
              ) : null}
              {moduleId && !module ? <p role="alert">Module not found in this tank.</p> : null}
            </div>
          ) : undefined
        }
        performance={
          <TankPerformance
            key={`${identifier}:${tankId}:${container.site?.id}`}
            containerIdentifier={identifier}
            tankId={view.tank.ref!.id}
            enabled={!error}
          />
        }
      />
      {module ? (
        <ModuleHistoryDialog
          key={`${identifier}:${tankId}:${moduleId}`}
          item={module}
          onRefresh={retry}
          onDismiss={() => navigate(path)}
        />
      ) : null}
    </>
  );
}
