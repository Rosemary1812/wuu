import { net, protocol } from "electron";
import { pathToFileURL } from "node:url";
import { pdfResponseHeaders, rangedPdfResponse } from "./renderableFileRange";
import {
  filePathFromRenderableURL,
  isRenderableImageFile,
  isRenderablePdfFile,
} from "./renderableFileURLs";

export function registerRenderableFileScheme(): void {
  protocol.registerSchemesAsPrivileged([
    {
      scheme: "wuu-file",
      privileges: {
        standard: true,
        secure: true,
        supportFetchAPI: false,
        // Chromium's built-in PDF viewer fetches the document from a
        // chrome-extension origin, which counts as cross-origin against this
        // scheme; without corsEnabled + an allow-origin header the viewer
        // renders a blank frame (Electron >=39 enforces this).
        corsEnabled: true,
        stream: true,
      },
    },
  ]);
}

export function registerRenderableFileProtocol(): void {
  protocol.handle("wuu-file", async (request) => {
    const filePath = filePathFromRenderableURL(request.url);
    if (!filePath) {
      return new Response("Not found", { status: 404 });
    }
    if (isRenderablePdfFile(filePath)) {
      // The viewer switches to range requests once it sees Accept-Ranges;
      // serving 206 chunks lets large PDFs render their first page without
      // downloading the whole document.
      const ranged = rangedPdfResponse(request, filePath);
      if (ranged) {
        return ranged;
      }
      const response = await net.fetch(pathToFileURL(filePath).toString());
      return new Response(response.body, {
        status: response.status,
        headers: pdfResponseHeaders(response.headers),
      });
    }
    if (!isRenderableImageFile(filePath)) {
      return new Response("Not found", { status: 404 });
    }
    return net.fetch(pathToFileURL(filePath).toString());
  });
}
