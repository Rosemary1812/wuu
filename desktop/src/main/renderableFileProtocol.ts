import { net, protocol } from "electron";
import { statSync } from "node:fs";
import { extname } from "node:path";
import { pathToFileURL } from "node:url";

const RENDERABLE_IMAGE_EXTENSIONS = new Set([
  ".apng",
  ".avif",
  ".gif",
  ".jpeg",
  ".jpg",
  ".png",
  ".svg",
  ".webp",
]);

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

function filePathFromRenderableURL(rawURL: string): string | undefined {
  try {
    const url = new URL(rawURL);
    if (url.hostname !== "local") {
      return undefined;
    }
    const encodedPath = url.pathname.replace(/^\/+/, "");
    if (!encodedPath) {
      return undefined;
    }
    return Buffer.from(encodedPath, "base64url").toString("utf8");
  } catch {
    return undefined;
  }
}

function isRenderableImageFile(filePath: string): boolean {
  try {
    return (
      statSync(filePath).isFile() &&
      RENDERABLE_IMAGE_EXTENSIONS.has(extname(filePath).toLowerCase())
    );
  } catch {
    return false;
  }
}
