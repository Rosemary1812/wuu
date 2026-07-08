import { net, protocol } from "electron";
import { pathToFileURL } from "node:url";
import {
  filePathFromRenderableURL,
  isRenderableImageFile,
} from "./renderableFileURLs";

export function registerRenderableFileScheme(): void {
  protocol.registerSchemesAsPrivileged([
    {
      scheme: "wuu-file",
      privileges: {
        standard: true,
        secure: true,
        supportFetchAPI: false,
      },
    },
  ]);
}

export function registerRenderableFileProtocol(): void {
  protocol.handle("wuu-file", (request) => {
    const filePath = filePathFromRenderableURL(request.url);
    if (!filePath || !isRenderableImageFile(filePath)) {
      return new Response("Not found", { status: 404 });
    }
    return net.fetch(pathToFileURL(filePath).toString());
  });
}
