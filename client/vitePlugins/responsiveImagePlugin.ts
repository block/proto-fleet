import { imageSizeFromFile } from "image-size/fromFile";
import { promises } from "fs";
import path from "path";

// This plugin processes image files to generate a default export with the image's URL and dimensions.
// It checks for the existence of a 2x version of the image and includes its URL if available.
export function responsiveImagePlugin() {
  return {
    name: "responsive-image",
    async transform(code, id) {
      // Only handle non-retina images and skip _2x images
      if (/\.(png|jpe?g|gif|webp|avif)$/.test(id) && !id.includes("_2x.")) {
        try {
          const dimensions = await imageSizeFromFile(id);
          const parsedPath = path.parse(id);
          const retinaPath = path.join(parsedPath.dir, `${parsedPath.name}_2x${parsedPath.ext}`);

          // Check if 2x version exists
          let retinaExists = false;
          try {
            await promises.access(retinaPath);
            retinaExists = true;
          } catch (err) {
            void err;
            // 2x version doesn't exist, that's fine for now
          }

          return {
            code: `
              const baseUrl = new URL(${JSON.stringify(id)}, import.meta.url).href;
              ${
                retinaExists
                  ? `const retinaUrl = new URL(${JSON.stringify(retinaPath)}, import.meta.url).href;`
                  : "const retinaUrl = null;"
              }
              
              export default {
                src: baseUrl,
                ${retinaExists ? "src2x: retinaUrl," : ""}
                width: ${dimensions.width},
                height: ${dimensions.height},
              };
            `,
            map: null,
          };
        } catch (err) {
          console.error("Error processing image:", id, err);
          return null;
        }
      }
    },
  };
}
