import { useEffect, useRef } from "react";

import useCssVariable from "@/shared/hooks/useCssVariable";

const LOGO_PATH_DATA =
  "M8.02671 0.101377C7.29433 0.254691 6.60478 0.647148 5.22567 1.43206L3.82683 2.22821C2.43299 3.02151 1.73607 3.41816 1.22908 3.97511C0.780513 4.46788 0.441524 5.04919 0.234254 5.68108C-9.46936e-06 6.39527 -6.29253e-06 7.19372 6.59957e-08 8.79062L4.12793e-06 9.81075C1.0516e-05 11.4151 1.37143e-05 12.2172 0.236159 12.934C0.445087 13.5682 0.78673 14.1512 1.2386 14.6447C1.74934 15.2024 2.45119 15.5979 3.85488 16.389L5.25726 17.1793L5.25727 17.1793C6.62558 17.9504 7.30973 18.336 8.03568 18.4869C8.6781 18.6204 9.34135 18.6201 9.98367 18.4862C10.7095 18.3348 11.3934 17.9488 12.7612 17.1767L14.1497 16.393C15.5518 15.6016 16.2528 15.2059 16.763 14.6483C17.2143 14.1549 17.5555 13.5722 17.7642 12.9383C18 12.2219 18 11.4204 18 9.81725L18 8.78938C18 7.19511 18 6.39797 17.7664 5.68472C17.5597 5.05365 17.2216 4.47294 16.7742 3.98042C16.2685 3.42376 15.5734 3.02673 14.183 2.23267L12.791 1.43768C11.4131 0.650716 10.7241 0.257236 9.99199 0.102836C9.34415 -0.0337854 8.67475 -0.0342821 8.02671 0.101377ZM6.45652 6.94689C5.15985 6.94689 4.1087 7.99805 4.1087 9.29472C4.1087 10.5914 5.15985 11.6425 6.45652 11.6425H11.5435C12.8401 11.6425 13.8913 10.5914 13.8913 9.29472C13.8913 7.99805 12.8401 6.94689 11.5435 6.94689H6.45652Z";
const VOLUME_DEPTH = 0.25;
const CROSS_SECTION_COUNT = 22;
const MAX_RENDER_RATIO = 1.5;
const TARGET_FRAME_INTERVAL = 1000 / 24;
const POINT_BUDGETS = { standard: 10_500, highDensity: 12_000 };
const BASE_RADIUS = 0.64;

interface Point {
  entropy: number;
  kind: "core" | "face";
  x: number;
  y: number;
  z: number;
}

interface ProjectedPoint extends Omit<Point, "x" | "y" | "z"> {
  perspective: number;
  x: number;
  y: number;
  z: number;
}

interface RenderBuffer {
  canvas: HTMLCanvasElement;
  ctx: CanvasRenderingContext2D;
  height: number;
  ratio: number;
  width: number;
}

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value));
const lerp = (a: number, b: number, amount: number) => a + (b - a) * amount;

const random = (seed: number) => {
  let value = seed >>> 0;
  return () => {
    value += 0x6d2b79f5;
    let next = value;
    next = Math.imul(next ^ (next >>> 15), next | 1);
    next ^= next + Math.imul(next ^ (next >>> 7), next | 61);
    return ((next ^ (next >>> 14)) >>> 0) / 4294967296;
  };
};

const createRenderBuffer = (): RenderBuffer | null => {
  const canvas = document.createElement("canvas");
  const ctx = canvas.getContext("2d", { alpha: true, desynchronized: true });
  return ctx ? { canvas, ctx, height: 0, ratio: 1, width: 0 } : null;
};

const resizeBuffer = (buffer: RenderBuffer, width: number, height: number, ratio: number) => {
  buffer.width = width;
  buffer.height = height;
  buffer.ratio = ratio;
  buffer.canvas.width = Math.round(width * ratio);
  buffer.canvas.height = Math.round(height * ratio);
  buffer.ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
};

const AvailableUpdateAnimation = () => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const foreground = useCssVariable("--color-core-primary-fill");
  const foregroundRef = useRef(foreground || "#000");
  const redrawStaticFrameRef = useRef<() => void>(() => {});

  useEffect(() => {
    foregroundRef.current = foreground || "#000";
    redrawStaticFrameRef.current();
  }, [foreground]);

  useEffect(() => {
    const canvas = canvasRef.current;
    const targetCtx = canvas?.getContext("2d", { alpha: true, desynchronized: true });
    if (!canvas || !targetCtx) return;

    const mask = createRenderBuffer();
    const tinted = createRenderBuffer();
    if (!mask || !tinted) return;

    const logoPath = typeof Path2D === "function" ? new Path2D(LOGO_PATH_DATA) : null;
    const sampleCanvas = document.createElement("canvas");
    const sampleCtx = sampleCanvas.getContext("2d");
    const svgPath = document.createElementNS("http://www.w3.org/2000/svg", "path");
    svgPath.setAttribute("d", LOGO_PATH_DATA);
    const reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    const points: Point[] = [];
    let pointStride = 1;
    let width = 0;
    let height = 0;
    let ratio = 1;
    let lastFrameNow = 0;
    let animationFrameId: number | null = null;
    let isVisible = true;

    const isInsideLogo = (x: number, y: number) =>
      !sampleCtx || !logoPath || sampleCtx.isPointInPath(logoPath, x, y, "evenodd");

    const addPoint = (x: number, y: number, z: number, kind: Point["kind"], entropy: number) => {
      points.push({ x: (x - 9) / 9.05, y: (y - 9.5) / 9.05, z, kind, entropy });
    };

    const buildPoints = () => {
      const rng = random(1337);
      const faceCount = 1800;
      const coreCount = 2200;
      const softBoundarySteps = 120;
      const softBoundaryLayers = 8;
      const volumeColumns = 32;
      const volumeRows = 34;

      for (let index = 0; index < faceCount; index += 1) {
        const x = rng() * 18;
        const y = rng() * 19;
        if (isInsideLogo(x, y)) addPoint(x, y, rng() > 0.5 ? VOLUME_DEPTH : -VOLUME_DEPTH, "face", rng());
      }

      for (let index = 0; index < coreCount; index += 1) {
        const x = rng() * 18;
        const y = rng() * 19;
        if (isInsideLogo(x, y)) addPoint(x, y, lerp(-VOLUME_DEPTH, VOLUME_DEPTH, rng()), "core", rng());
      }

      for (let layer = 0; layer < CROSS_SECTION_COUNT; layer += 1) {
        const z = lerp(-VOLUME_DEPTH, VOLUME_DEPTH, layer / (CROSS_SECTION_COUNT - 1)) + (rng() - 0.5) * 0.035;
        for (let col = 0; col < volumeColumns; col += 1) {
          for (let row = 0; row < volumeRows; row += 1) {
            const stagger = row % 2 === 0 ? 0.18 : -0.18;
            const x = ((col + 0.5 + stagger + (rng() - 0.5) * 0.2) / volumeColumns) * 18;
            const y = ((row + 0.5 + (layer % 2) * 0.18 + (rng() - 0.5) * 0.2) / volumeRows) * 19;
            if (!isInsideLogo(x, y) || (layer > 0 && layer < CROSS_SECTION_COUNT - 1 && rng() < 0.06)) continue;
            addPoint(x, y, z, "core", rng());
          }
        }
      }

      try {
        const length = svgPath.getTotalLength();
        for (let step = 0; step < softBoundarySteps; step += 1) {
          const pathPoint = svgPath.getPointAtLength(((step + rng() * 0.35) / softBoundarySteps) * length);
          for (let layer = 0; layer < softBoundaryLayers; layer += 1) {
            const angle = rng() * Math.PI * 2;
            const scatter = 0.18 + rng() * 0.36;
            const x = pathPoint.x + Math.cos(angle) * scatter;
            const y = pathPoint.y + Math.sin(angle) * scatter;
            if (!isInsideLogo(x, y)) continue;
            const z = lerp(-VOLUME_DEPTH, VOLUME_DEPTH, (layer + rng() * 0.25) / (softBoundaryLayers - 1 + 0.25));
            addPoint(x, y, z, "core", rng());
          }
        }
      } catch {
        for (let index = 0; index < 520; index += 1) {
          const angle = (index / 520) * Math.PI * 2;
          addPoint(
            9 + Math.cos(angle) * 8.5,
            9.5 + Math.sin(angle) * 8.85,
            lerp(-VOLUME_DEPTH, VOLUME_DEPTH, rng()),
            "core",
            rng(),
          );
        }
      }
    };

    const rotatePoint = (point: Point, time: number) => {
      const yaw = time * 0.86;
      const cosY = Math.cos(yaw);
      const sinY = Math.sin(yaw);
      return { ...point, x: point.x * cosY - point.z * sinY, z: point.x * sinY + point.z * cosY };
    };

    const projectFrame = (time: number) => {
      const minSize = Math.min(width, height);
      const scale = Math.min(minSize, 240) * 0.42;
      const focal = 2.7;
      const camera = 3.55 + Math.sin(time * 0.24) * 0.08;
      const projected: ProjectedPoint[] = [];

      for (let index = 0; index < points.length; index += pointStride) {
        const rotated = rotatePoint(points[index], time);
        const z = camera - rotated.z;
        if (z <= 0.35) continue;
        const perspective = focal / z;
        const x = width * 0.5 + rotated.x * scale * perspective;
        const y = height * 0.5 + rotated.y * scale * perspective;
        if (x < -40 || x > width + 40 || y < -40 || y > height + 40) continue;
        projected.push({ ...rotated, x, y, z, perspective });
      }

      return { camera, focal, minSize, projected, scale };
    };

    const drawCrossSectionGlow = (
      ctx: CanvasRenderingContext2D,
      time: number,
      frame: ReturnType<typeof projectFrame>,
    ) => {
      if (!logoPath) return;
      const yaw = time * 0.86;
      const cosY = Math.cos(yaw);
      const sinY = Math.sin(yaw);
      const xCompression = clamp(Math.abs(cosY), 0.2, 1);

      ctx.save();
      ctx.filter = "blur(7px)";
      ctx.fillStyle = "#fff";
      for (let layer = 0; layer < CROSS_SECTION_COUNT; layer += 1) {
        const amount = layer / (CROSS_SECTION_COUNT - 1);
        const rotatedX = -lerp(-VOLUME_DEPTH, VOLUME_DEPTH, amount) * sinY;
        const rotatedZ = lerp(-VOLUME_DEPTH, VOLUME_DEPTH, amount) * cosY;
        const z = frame.camera - rotatedZ;
        if (z <= 0.35) continue;
        const perspective = frame.focal / z;
        const centerWeight = 1 - Math.abs(amount * 2 - 1);
        const near = clamp(1 - (z - 2.1) / 2.25, 0, 1);

        ctx.save();
        ctx.globalAlpha = (0.008 + near * 0.014) * (0.62 + centerWeight * 0.38);
        ctx.translate(width * 0.5 + rotatedX * frame.scale * perspective, height * 0.5);
        ctx.scale((frame.scale * perspective * xCompression) / 9.05, (frame.scale * perspective) / 9.05);
        ctx.translate(-9, -9.5);
        ctx.fill(logoPath, "evenodd");
        ctx.restore();
      }
      ctx.restore();
    };

    const renderFrame = (time: number) => {
      const frame = projectFrame(time);
      const maskCtx = mask.ctx;
      maskCtx.setTransform(ratio, 0, 0, ratio, 0, 0);
      maskCtx.globalAlpha = 1;
      maskCtx.filter = "none";
      maskCtx.clearRect(0, 0, width, height);
      drawCrossSectionGlow(maskCtx, time, frame);

      const bucketCount = 18;
      const buckets = Array.from({ length: bucketCount }, () => null as Path2D | null);
      for (const point of frame.projected) {
        const near = clamp(1 - (point.z - 2.1) / 2.25, 0, 1);
        const glint = Math.pow(0.5 + Math.sin(time * 2.4 + point.entropy * 30) * 0.5, 3);
        const alpha = clamp((0.025 + near * 0.22 + glint * 0.08) * (point.kind === "face" ? 0.96 : 0.9), 0.018, 0.44);
        const radius = BASE_RADIUS * point.perspective * (1 + glint * 0.24);
        const bucket = clamp(Math.round(alpha * (bucketCount - 1)), 1, bucketCount - 1);
        buckets[bucket] ??= new Path2D();
        const bucketPath = buckets[bucket];
        bucketPath?.moveTo(point.x + radius, point.y);
        bucketPath?.arc(point.x, point.y, radius, 0, Math.PI * 2);
      }
      for (let bucket = 1; bucket < bucketCount; bucket += 1) {
        const bucketPath = buckets[bucket];
        if (!bucketPath) continue;
        maskCtx.fillStyle = `rgb(255 255 255 / ${bucket / (bucketCount - 1)})`;
        maskCtx.fill(bucketPath);
      }

      const haze = maskCtx.createRadialGradient(
        width * 0.5,
        height * 0.5,
        frame.minSize * 0.12,
        width * 0.5,
        height * 0.5,
        frame.minSize * 0.64,
      );
      haze.addColorStop(0, "rgb(255 255 255 / 3.5%)");
      haze.addColorStop(0.52, "rgb(255 255 255 / 1.4%)");
      haze.addColorStop(1, "rgb(255 255 255 / 0%)");
      maskCtx.fillStyle = haze;
      maskCtx.fillRect(0, 0, width, height);

      const tintedCtx = tinted.ctx;
      tintedCtx.setTransform(ratio, 0, 0, ratio, 0, 0);
      tintedCtx.clearRect(0, 0, width, height);
      tintedCtx.drawImage(mask.canvas, 0, 0, width, height);
      tintedCtx.globalCompositeOperation = "source-in";
      tintedCtx.fillStyle = foregroundRef.current;
      tintedCtx.fillRect(0, 0, width, height);
      tintedCtx.globalCompositeOperation = "source-over";

      targetCtx.setTransform(ratio, 0, 0, ratio, 0, 0);
      targetCtx.clearRect(0, 0, width, height);
      targetCtx.drawImage(tinted.canvas, 0, 0, width, height);
    };

    const renderStaticFrame = () => renderFrame(0);
    redrawStaticFrameRef.current = () => {
      if (reducedMotion?.matches) renderStaticFrame();
    };
    const shouldAnimate = () => isVisible && !document.hidden && !reducedMotion?.matches;
    const requestDraw = () => {
      if (animationFrameId === null && shouldAnimate()) animationFrameId = requestAnimationFrame(draw);
    };
    const draw = (now: number) => {
      animationFrameId = null;
      if (!shouldAnimate()) return;
      if (lastFrameNow && now - lastFrameNow < TARGET_FRAME_INTERVAL) {
        requestDraw();
        return;
      }
      renderFrame(now * 0.001);
      lastFrameNow = now - ((now - lastFrameNow) % TARGET_FRAME_INTERVAL);
      requestDraw();
    };
    const updatePlayback = () => {
      if (shouldAnimate()) requestDraw();
      else if (reducedMotion?.matches) renderStaticFrame();
    };
    const resize = () => {
      const bounds = canvas.getBoundingClientRect();
      width = Math.max(1, Math.round(bounds.width));
      height = Math.max(1, Math.round(bounds.height));
      ratio = Math.min(window.devicePixelRatio || 1, MAX_RENDER_RATIO);
      pointStride = Math.max(
        1,
        Math.ceil(points.length / (window.devicePixelRatio > 1 ? POINT_BUDGETS.highDensity : POINT_BUDGETS.standard)),
      );
      canvas.width = Math.round(width * ratio);
      canvas.height = Math.round(height * ratio);
      targetCtx.setTransform(ratio, 0, 0, ratio, 0, 0);
      resizeBuffer(mask, width, height, ratio);
      resizeBuffer(tinted, width, height, ratio);
      if (reducedMotion?.matches) renderStaticFrame();
    };

    buildPoints();
    resize();
    const resizeObserver = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(resize);
    resizeObserver?.observe(canvas);
    const intersectionObserver =
      typeof IntersectionObserver === "undefined"
        ? null
        : new IntersectionObserver(([entry]) => {
            isVisible = entry.isIntersecting;
            updatePlayback();
          });
    intersectionObserver?.observe(canvas);
    window.addEventListener("resize", resize);
    document.addEventListener("visibilitychange", updatePlayback);
    reducedMotion?.addEventListener("change", updatePlayback);
    updatePlayback();

    return () => {
      if (animationFrameId !== null) cancelAnimationFrame(animationFrameId);
      resizeObserver?.disconnect();
      intersectionObserver?.disconnect();
      window.removeEventListener("resize", resize);
      document.removeEventListener("visibilitychange", updatePlayback);
      reducedMotion?.removeEventListener("change", updatePlayback);
      redrawStaticFrameRef.current = () => {};
    };
  }, []);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      data-testid="available-update-animation"
      className="h-[240px] min-h-[240px] w-full shrink-0 bg-transparent tablet:h-auto tablet:w-[30%] tablet:self-stretch"
    />
  );
};

export default AvailableUpdateAnimation;
