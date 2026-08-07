/**
 * The collapsible "Data flow" drawer + its provider.
 *
 * Pages pull a tracer from context (`useFlowTrace().makeTracer(transport)`),
 * reset before a connect, and hand the tracer to the adapter. The adapter emits
 * events as it runs; this pane renders them live, color-coded by hop (see
 * flowTrace.ts) — an at-a-glance narration of what each strategy actually does.
 */
import { createContext, ReactNode, useCallback, useContext, useMemo, useRef, useState } from "react";

import { CHANNEL_META, type FlowChannel, type FlowEvent, type FlowTracer, type Transport } from "./flowTrace";
import { ArrowRight, Dismiss } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants as buttonVariants } from "@/shared/components/Button";

interface FlowTraceContextValue {
  events: FlowEvent[];
  open: boolean;
  setOpen: (open: boolean) => void;
  reset: () => void;
  makeTracer: (transport: Transport) => FlowTracer;
}

const FlowTraceContext = createContext<FlowTraceContextValue | null>(null);

export function useFlowTrace(): FlowTraceContextValue {
  const ctx = useContext(FlowTraceContext);
  if (!ctx) throw new Error("useFlowTrace must be used within a FlowTraceProvider");
  return ctx;
}

export function FlowTraceProvider({ children }: { children: ReactNode }) {
  const [events, setEvents] = useState<FlowEvent[]>([]);
  const [open, setOpen] = useState(true);
  const idRef = useRef(0);

  const add = useCallback((e: FlowEvent) => setEvents((prev) => [...prev, e]), []);
  const update = useCallback(
    (id: number, patch: Partial<FlowEvent>) =>
      setEvents((prev) => prev.map((e) => (e.id === id ? { ...e, ...patch } : e))),
    [],
  );
  const reset = useCallback(() => setEvents([]), []);

  const makeTracer = useCallback(
    (transport: Transport): FlowTracer => ({
      request: (target, title, detail) => {
        const id = (idRef.current += 1);
        add({
          id,
          channel: target === "fleet" ? "smv-fleet" : "smv-miner",
          title,
          detail,
          method: /^(GET|POST|PUT|PATCH|DELETE)\b/.exec(title)?.[0],
          proxied: target === "miner" && transport === "proxy",
          status: "pending",
        });
        return {
          ok: (d) => update(id, { status: "ok", detail: d ?? detail }),
          fail: (m) => update(id, { status: "error", detail: m ?? detail }),
        };
      },
      seam: (title, detail) => add({ id: (idRef.current += 1), channel: "seam", title, detail, status: "ok" }),
      adapter: (title, detail) => {
        // The adapter mapping is the story for the S3 abstraction prototype (its
        // fleet + direct contexts). S2 (proxy) leads with the version seam, and
        // S1 (fleet-native) reads the proto directly with no adapter — suppress
        // the adapter row in both so each prototype's story stays distinct.
        if (transport === "proxy" || transport === "fleet-native") return;
        add({ id: (idRef.current += 1), channel: "adapter", title, detail, status: "ok" });
      },
      note: (channel, title, detail) => add({ id: (idRef.current += 1), channel, title, detail, status: "ok" }),
    }),
    [add, update],
  );

  const value = useMemo(() => ({ events, open, setOpen, reset, makeTracer }), [events, open, reset, makeTracer]);

  return <FlowTraceContext.Provider value={value}>{children}</FlowTraceContext.Provider>;
}

const STATUS_GLYPH: Record<FlowEvent["status"], { mark: string; className: string }> = {
  pending: { mark: "running…", className: "text-text-primary-30" },
  ok: { mark: "✓", className: "text-intent-success-fill" },
  error: { mark: "✕", className: "text-intent-critical-fill" },
};

function LegendItem({ channel }: { channel: FlowChannel }) {
  const meta = CHANNEL_META[channel];
  return (
    <div className="flex items-center gap-2">
      <span className={`h-2 w-2 rounded-full ${meta.dot}`} />
      <span className="text-heading-100 text-text-primary-50">{meta.label}</span>
    </div>
  );
}

const METHOD_BADGE: Record<string, string> = {
  GET: "bg-surface-10 text-text-primary-50",
  POST: "bg-text-primary text-surface-base",
  PUT: "bg-text-primary text-surface-base",
  PATCH: "bg-text-primary text-surface-base",
  DELETE: "bg-text-primary text-surface-base",
};

function EventRow({ event }: { event: FlowEvent }) {
  const meta = CHANNEL_META[event.channel];
  const status = STATUS_GLYPH[event.status];
  // A write (POST/…) gets a solid pill; a read (GET) a muted one — so mutations
  // stand out from the reads around them regardless of channel color.
  const method = event.method;
  const title = method ? event.title.slice(method.length).trim() : event.title;
  return (
    <div className={`flex flex-col gap-1 border-l-2 py-2 pl-3 ${meta.bar} ${meta.tint}`}>
      <div className="flex items-start justify-between gap-2">
        <span className="flex min-w-0 items-center gap-1.5">
          {method ? (
            <span className={`shrink-0 rounded px-1 text-[10px] font-semibold ${METHOD_BADGE[method]}`}>{method}</span>
          ) : null}
          <span className="truncate text-200 text-text-primary">{title}</span>
        </span>
        <span className={`shrink-0 text-heading-100 ${status.className}`}>{status.mark}</span>
      </div>
      {event.detail ? <span className="text-heading-100 text-text-primary-50">{event.detail}</span> : null}
      <div className="flex flex-wrap items-center gap-1.5">
        <span className={`text-[10px] ${meta.text}`}>{meta.label}</span>
        {event.proxied ? (
          <span className="rounded-full bg-surface-10 px-1.5 text-[10px] text-text-primary-50">via fleet proxy</span>
        ) : null}
      </div>
    </div>
  );
}

/** Fixed right drawer; width is managed by the parent shell's padding. */
export function FlowPane() {
  const { events, open, setOpen } = useFlowTrace();

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="fixed top-1/2 right-0 z-40 flex -translate-y-1/2 items-center gap-1 rounded-l-md bg-surface-elevated-base px-2 py-3 shadow-100"
        aria-label="Show data flow"
      >
        <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase [writing-mode:vertical-rl]">
          Data flow
        </span>
      </button>
    );
  }

  return (
    <aside className="fixed top-0 right-0 z-40 flex h-screen w-80 flex-col border-l border-border-5 bg-surface-elevated-base shadow-100">
      <div className="flex items-center justify-between border-b border-border-5 px-4 py-3">
        <span className="text-heading-200 text-text-primary">Data flow</span>
        <Button
          ariaLabel="Hide data flow"
          prefixIcon={<Dismiss />}
          onClick={() => setOpen(false)}
          size={buttonSizes.compact}
          variant={buttonVariants.ghost}
        />
      </div>

      <div className="flex flex-col gap-1.5 border-b border-border-5 px-4 py-3">
        <LegendItem channel="smv-fleet" />
        <LegendItem channel="fleet-miner" />
        <LegendItem channel="smv-miner" />
        <LegendItem channel="seam" />
        <LegendItem channel="adapter" />
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-3">
        {events.length === 0 ? (
          <div className="flex flex-col items-center gap-2 pt-10 text-center">
            <ArrowRight className="w-4 text-text-primary-30" />
            <span className="text-200 text-text-primary-50">
              Connect to a miner to trace the requests, proxy hops, version seam, and adapter layer behind the view.
            </span>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {events.map((e) => (
              <EventRow key={e.id} event={e} />
            ))}
          </div>
        )}
      </div>
    </aside>
  );
}
