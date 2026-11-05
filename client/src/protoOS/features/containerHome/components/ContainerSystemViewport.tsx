import { type ReactNode, useId } from "react";
import clsx from "clsx";
import { useSchematicViewport } from "../hooks/useSchematicViewport";
import { MAX_ZOOM, minimumScale, SCHEMATIC_HEIGHT, SCHEMATIC_WIDTH } from "./systemDiagramView";
import Button from "@/shared/components/Button";

export default function ContainerSystemViewport({
  children,
  className,
  actions,
}: {
  children: ReactNode;
  className?: string;
  actions?: ReactNode;
}) {
  const { view, size, dragging, viewportRef, fit, zoomIn, zoomOut } = useSchematicViewport();
  const helpId = useId();
  const ready = size.width > 0;

  return (
    <section
      aria-label="Container cooling system schematic"
      className={clsx("min-w-0 overflow-hidden rounded-3xl bg-surface-elevated-base shadow-100", className)}
    >
      <div className="flex flex-wrap items-center justify-end gap-3 px-4 pt-3">
        {actions ? <div className="mr-auto">{actions}</div> : null}
        <div role="group" aria-label="Diagram view" className="flex items-center gap-1.5">
          <Button
            variant="secondary"
            size="compact"
            text="−"
            ariaLabel="Zoom out"
            className="min-h-10 min-w-10"
            disabled={!ready || view.scale <= minimumScale(size) + 0.001}
            onClick={zoomOut}
          />
          <output aria-label="Diagram zoom level" aria-live="off" className="w-14 text-center text-200 tabular-nums">
            {ready ? `${Math.round(view.scale * 100)}%` : "—"}
          </output>
          <Button
            variant="secondary"
            size="compact"
            text="+"
            ariaLabel="Zoom in"
            className="min-h-10 min-w-10"
            disabled={!ready || view.scale >= MAX_ZOOM - 0.001}
            onClick={zoomIn}
          />
          <Button
            variant="secondary"
            size="compact"
            text="Fit"
            ariaLabel="Fit whole diagram"
            className="min-h-10 min-w-10"
            disabled={!ready}
            onClick={fit}
          />
        </div>
      </div>
      <div
        ref={viewportRef}
        role="group"
        aria-label="Diagram canvas"
        aria-describedby={helpId}
        tabIndex={0}
        className={clsx(
          "relative h-[400px] w-full touch-none overflow-clip outline-none select-none focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-inset tablet:h-[clamp(300px,65vh,680px)]",
          dragging ? "cursor-grabbing [&_*]:cursor-grabbing" : "cursor-grab",
        )}
      >
        <div
          data-testid="system-diagram-content"
          className="absolute top-0 left-0 origin-top-left will-change-transform"
          style={{
            width: SCHEMATIC_WIDTH,
            height: SCHEMATIC_HEIGHT,
            opacity: ready ? 1 : 0,
            transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})`,
          }}
        >
          {children}
        </div>
      </div>
      <div
        id={helpId}
        className="flex flex-wrap justify-between gap-x-6 gap-y-2 px-4 pt-3 pb-4 text-200 text-text-primary-50"
      >
        <span>Drag to pan · Pinch or Ctrl/⌘ + scroll to zoom · Fit to reset</span>
        <span>Keyboard: + / − to zoom, arrows to pan, 0 to fit</span>
      </div>
    </section>
  );
}
