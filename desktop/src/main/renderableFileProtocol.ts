import { net, protocol } from "electron";
import { pathToFileURL } from "node:url";
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
      const response = await net.fetch(pathToFileURL(filePath).toString());
      const headers = new Headers(response.headers);
      headers.set("content-type", "application/pdf");
      headers.set("access-control-allow-origin", "*");
      return new Response(response.body, {
        status: response.status,
        headers,
      });
    }
    if (!isRenderableImageFile(filePath)) {
      return new Response("Not found", { status: 404 });
    }
    return net.fetch(pathToFileURL(filePath).toString());
  });
}
