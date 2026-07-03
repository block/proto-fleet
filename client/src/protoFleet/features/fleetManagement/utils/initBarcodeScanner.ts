import { prepareZXingModule } from "barcode-detector/ponyfill";
// Import the ZXing reader WASM as a same-origin, content-hashed asset URL.
// Vite emits this into the build output and rewrites the import to the final
// hashed path, so the binary is served from the Fleet host itself. This is
// what makes scanning work on air-gapped / on-prem installs: the library's
// default behavior is to fetch the .wasm from the public jsDelivr CDN, which
// is unreachable at a locked-down data center.
import zxingReaderWasmUrl from "zxing-wasm/reader/zxing_reader.wasm?url";

let prepared = false;

/**
 * Point the barcode scanner's WASM loader at our self-hosted, same-origin
 * binary. Idempotent and safe to call before every scan session; the actual
 * fetch/compile happens lazily inside the library on first detect().
 *
 * Must be called before constructing a `BarcodeDetector` that will fall back
 * to WASM (i.e. any non-Android browser). On Android, where the native
 * BarcodeDetector is used, the WASM is never fetched and this override is a
 * no-op in practice.
 */
export function initBarcodeScanner(): void {
  if (prepared) return;
  prepared = true;

  prepareZXingModule({
    overrides: {
      locateFile: (path: string, prefix: string) => {
        if (path.endsWith(".wasm")) {
          return zxingReaderWasmUrl;
        }
        // Preserve default resolution for any non-wasm asset.
        return prefix + path;
      },
    },
  });
}
