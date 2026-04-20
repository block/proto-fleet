import { ReactNode } from "react";
import { ImageMetadata } from "@/shared/components/Picture";

type BackgroundImageProps = {
  image: ImageMetadata | string;
  alt?: string;
  className?: string;
  backgroundPosition?: string;
  children?: ReactNode;
};

const BackgroundImage = ({
  image,
  alt,
  className,
  backgroundPosition = "unset unset",
  children,
}: BackgroundImageProps) => {
  // if image was not processed by vite plugin the import will be a string
  if (typeof image === "string") {
    return (
      <div className={className} title={alt} style={{ backgroundImage: image }}>
        {children}
      </div>
    );
  }

  const { src, src2x } = image;
  const srcSet = src2x ? `image-set(url(${src}) 1x, url(${src2x}) 2x)` : `url(${src})`;

  return (
    <div
      className={className}
      title={alt}
      style={{
        backgroundRepeat: "no-repeat",
        backgroundSize: "cover",
        backgroundPosition: backgroundPosition,
        backgroundImage: srcSet,
      }}
    >
      {children}
    </div>
  );
};

export default BackgroundImage;
