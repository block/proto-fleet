interface ObjectCoverSourceCropInput {
  sourceWidth: number;
  sourceHeight: number;
  renderedWidth: number;
  renderedHeight: number;
}

export interface SourceCrop {
  sx: number;
  sy: number;
  sw: number;
  sh: number;
}

/**
 * Return the source rectangle visible when an intrinsic image/video is rendered
 * with CSS `object-fit: cover` into the given box.
 */
export function getObjectCoverSourceCrop({
  sourceWidth,
  sourceHeight,
  renderedWidth,
  renderedHeight,
}: ObjectCoverSourceCropInput): SourceCrop | null {
  if (!sourceWidth || !sourceHeight || !renderedWidth || !renderedHeight) return null;

  const sourceAspect = sourceWidth / sourceHeight;
  const renderedAspect = renderedWidth / renderedHeight;

  if (sourceAspect > renderedAspect) {
    const sw = sourceHeight * renderedAspect;
    return {
      sx: (sourceWidth - sw) / 2,
      sy: 0,
      sw,
      sh: sourceHeight,
    };
  }

  if (sourceAspect < renderedAspect) {
    const sh = sourceWidth / renderedAspect;
    return {
      sx: 0,
      sy: (sourceHeight - sh) / 2,
      sw: sourceWidth,
      sh,
    };
  }

  return {
    sx: 0,
    sy: 0,
    sw: sourceWidth,
    sh: sourceHeight,
  };
}
