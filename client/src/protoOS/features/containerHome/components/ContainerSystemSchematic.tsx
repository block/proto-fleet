import type { ReactNode } from "react";
import clsx from "clsx";
import ContainerSystemViewport from "./ContainerSystemViewport";
import { Alert, ArrowRight, Reboot, Speedometer, Thermometer } from "@/shared/assets/icons";
import RadialGauge from "@/shared/components/RadialGauge";

export interface SystemTankReading {
  id: string;
  label: string;
  /** Visual fill only; null renders the tank as unavailable. */
  levelPercent: number | null;
}

export interface SystemTankBank {
  id: string;
  tanks: SystemTankReading[];
  temperature: string;
  pressure?: string;
  /** Raw bank-level measurement when a calibrated fill percentage is unavailable. */
  fluidLevel?: string;
}

export interface SystemFanReading {
  id: string;
  label: string;
  speedPercent: number | null;
  status?: "normal" | "critical";
}

export interface ContainerSystemSchematicProps {
  tankBanks: [SystemTankBank, SystemTankBank];
  fans: SystemFanReading[];
  flowRate: string;
  flowAlert?: boolean;
  pumpSpeed: string;
  dryCoolerInletTemperature: string;
  dryCoolerOutletPressure: string;
  pumpOutletPressure: string;
  className?: string;
  actions?: ReactNode;
}

const COLD_BLUE = "#31a9df";
const HOT_RED = "var(--color-text-critical)";

const clampPercent = (value: number) => Math.max(0, Math.min(100, value));

function NumberBadge({ children }: { children: string }) {
  return (
    <span aria-hidden="true" className="block size-6 shrink-0 rounded-full bg-core-primary-10 text-text-primary-70">
      <svg viewBox="0 0 24 24" className="size-6" aria-hidden="true">
        <text
          x="12"
          y="12"
          fill="currentColor"
          fontSize="12"
          fontWeight="700"
          textAnchor="middle"
          dominantBaseline="central"
        >
          {children}
        </text>
      </svg>
    </span>
  );
}

function Tank({ tank }: { tank: SystemTankReading }) {
  const level = tank.levelPercent === null ? 0 : clampPercent(tank.levelPercent);
  const vessel = (
    <span
      className={clsx(
        "relative block h-[97px] w-[110px] overflow-hidden rounded-xl bg-core-primary-10",
        tank.levelPercent === null && "border border-border-10 bg-surface-5",
      )}
    >
      {tank.levelPercent === null ? null : (
        <span className="absolute inset-x-0 bottom-0 bg-text-primary-70" style={{ height: `${level}%` }} />
      )}
    </span>
  );

  return (
    <div
      role="img"
      aria-label={`${tank.label}: ${tank.levelPercent === null ? "level unavailable" : `${level}% full`}`}
      className="flex h-[125px] min-w-0 flex-1 flex-col items-center justify-center gap-2"
    >
      {vessel}
      <NumberBadge>{tank.label}</NumberBadge>
    </div>
  );
}

function TankBank({ bank, lower }: { bank: SystemTankBank; lower?: boolean }) {
  const reading = (
    <div className="flex justify-center gap-4 font-mono text-[13px] text-text-primary">
      <span>
        <span className="mr-1 font-sans text-[10px] font-semibold tracking-wide text-text-primary-50">TEMP</span>
        {bank.temperature}
      </span>
      {bank.pressure ? (
        <span>
          <span className="mr-1 font-sans text-[10px] font-semibold tracking-wide text-text-primary-50">PRESS</span>
          {bank.pressure}
        </span>
      ) : null}
      {bank.fluidLevel ? (
        <span>
          <span className="mr-1 font-sans text-[10px] font-semibold text-text-primary-50">LEVEL</span>
          {bank.fluidLevel}
        </span>
      ) : null}
    </div>
  );

  return (
    <section
      aria-label={`${bank.id}: ${[bank.temperature, bank.pressure, bank.fluidLevel].filter(Boolean).join(", ")}`}
      className="absolute left-0 flex h-[212px] w-[402px] flex-col justify-center gap-6 rounded-3xl border border-border-10 bg-surface-base p-6"
      style={{ top: lower ? 328 : 68 }}
    >
      <span className="absolute top-6 left-6 text-[12px] font-semibold text-text-primary-70">{bank.id}</span>
      {reading}
      <div className="flex gap-3">
        {bank.tanks.map((tank) => (
          <Tank key={tank.id} tank={tank} />
        ))}
      </div>
    </section>
  );
}

function FanGauge({ fan }: { fan: SystemFanReading }) {
  const value = fan.speedPercent === null ? 0 : Math.round(clampPercent(fan.speedPercent));
  const critical = fan.status === "critical";
  return (
    <div
      role="img"
      aria-label={`${fan.label}: ${fan.speedPercent === null ? "speed unavailable" : `${value}%`}${critical ? ", critical" : ""}`}
      className="grid h-[68px] min-w-0 grid-cols-[40px] grid-rows-[24px_40px] justify-center justify-items-center gap-y-1"
    >
      <NumberBadge>{fan.label}</NumberBadge>
      <RadialGauge
        value={value}
        size={40}
        strokeWidth={4}
        valueStrokeWidth={4}
        strokeLinecap="butt"
        trackOpacity={0.08}
        colorClassName={critical ? "text-text-critical" : "text-text-primary"}
        centerText={fan.speedPercent === null ? "—" : `${value}%`}
        centerTextFontSize={12}
        centerTextClassName={clsx("font-mono", critical && "font-semibold")}
      />
    </div>
  );
}

function FanArray({ fans }: { fans: SystemFanReading[] }) {
  return (
    <section
      aria-label="Dry cooler fans"
      className="absolute top-[170px] left-[650px] grid h-[200px] w-[430px] grid-cols-5 grid-rows-2 place-items-center gap-x-5 gap-y-3 rounded-3xl border border-border-10 bg-surface-base px-6 py-5"
    >
      <span className="absolute -top-7 left-0 text-[12px] font-semibold text-text-primary-70">Dry cooler</span>
      {fans.slice(0, 10).map((fan) => (
        <FanGauge key={fan.id} fan={fan} />
      ))}
    </section>
  );
}

function Instrument({
  icon,
  value,
  label,
  alert = false,
  className,
}: {
  icon: ReactNode;
  value?: string;
  label: string;
  alert?: boolean;
  className: string;
}) {
  return (
    <div
      role="img"
      aria-label={`${label}${value ? `: ${value}` : ""}${alert ? ", alert" : ""}`}
      className={clsx("absolute flex w-36 -translate-x-1/2 translate-y-0.5 flex-col items-center gap-2", className)}
    >
      <span className="absolute bottom-full mb-3 flex w-full items-center justify-center gap-1 text-center text-[11px] leading-tight font-semibold text-text-primary-70">
        {label}
      </span>
      <span
        className={clsx(
          "grid size-10 place-items-center rounded-full bg-surface-base shadow-100",
          alert ? "text-text-critical" : "text-text-primary",
        )}
      >
        {icon}
      </span>
      {value ? <span className="font-mono text-[13px] whitespace-nowrap">{value}</span> : null}
    </div>
  );
}

function Pipework() {
  return (
    <svg aria-hidden="true" className="absolute inset-0" width="1120" height="600" viewBox="0 0 1120 600" fill="none">
      <g stroke={HOT_RED} strokeWidth="6" strokeLinecap="round" strokeLinejoin="round">
        <path d="M56 68V42H468Q480 42 480 54V418Q480 430 492 430H1088Q1100 430 1100 418V332Q1100 320 1088 320H1080" />
        <path d="M164 68V42M272 68V42" />
        <path d="M56 328V304H480M171 328V304M287 328V304" strokeLinecap="butt" />
      </g>

      <g stroke={COLD_BLUE} strokeWidth="6" strokeLinecap="round" strokeLinejoin="round">
        <path d="M56 280V292H446" strokeLinecap="butt" />
        <path d="M171 280V292M287 280V292" />
        <path d="M56 540V562H434Q446 562 446 550V124Q446 112 458 112H1088Q1100 112 1100 124V208Q1100 220 1088 220H1080" />
        <path d="M171 540V562M287 540V562" />
      </g>

      <polygon points="648,112 662,104 662,120" fill={COLD_BLUE} />
      <polygon points="1020,430 1006,422 1006,438" fill={HOT_RED} />
    </svg>
  );
}

export default function ContainerSystemSchematic({
  tankBanks,
  fans,
  flowRate,
  flowAlert = false,
  pumpSpeed,
  dryCoolerInletTemperature,
  dryCoolerOutletPressure,
  pumpOutletPressure,
  className,
  actions,
}: ContainerSystemSchematicProps) {
  return (
    <ContainerSystemViewport className={className} actions={actions}>
      <div className="relative h-[600px] w-[1120px] bg-surface-elevated-base text-text-primary">
        <Pipework />
        <TankBank bank={tankBanks[0]} />
        <TankBank bank={tankBanks[1]} lower />
        <FanArray fans={fans} />

        <span className="absolute top-[79px] left-[500px] text-[12px] font-semibold" style={{ color: COLD_BLUE }}>
          ← Tank inlet
        </span>
        <span className="absolute top-[368px] left-[500px] text-[12px] font-semibold" style={{ color: HOT_RED }}>
          Tank outlet →
        </span>

        <Instrument icon={<Reboot />} value={pumpSpeed} label="Pump speed" className="top-[410px] left-[540px]" />
        <Instrument
          icon={<Speedometer />}
          value={pumpOutletPressure}
          label="Pump outlet pressure"
          className="top-[410px] left-[700px]"
        />
        <Instrument
          icon={flowAlert ? <Alert /> : <ArrowRight />}
          value={flowRate}
          label="Flow rate"
          alert={flowAlert}
          className="top-[410px] left-[860px]"
        />
        <Instrument
          icon={<Thermometer />}
          value={dryCoolerInletTemperature}
          label="Dry cooler inlet temperature"
          className="top-[410px] left-[1020px]"
        />
        <Instrument
          icon={<Speedometer />}
          value={dryCoolerOutletPressure}
          label="Dry cooler outlet pressure"
          className="top-[92px] left-[860px]"
        />
      </div>
    </ContainerSystemViewport>
  );
}
