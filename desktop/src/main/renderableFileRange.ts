import { createReadStream, statSync } from "node:fs";
import { Readable } from "node:stream";

// Chromium's built-in PDF viewer issues byte-range requests once the
// response advertises `Accept-Ranges: bytes`. Without range support the
// viewer has to download the whole document before it can render the first
// page; with it, the first page renders after the first chunk + xref fetch.

export type ByteRange = { start: number; end: number };

// Parses a single `Range: bytes=...` header. Multi-range requests are
// ignored (callers fall back to a full 200 body) because PDF viewers never
// send them and multipart/byteranges bodies are not worth the complexity.
export function parseByteRangeHeader(
  header: string | null,
  size: number,
): ByteRange | "unsatisfiable" | undefined {
  if (!header) {
    return undefined;
  }
  const match = /^bytes=(\d*)-(\d*)$/i.exec(header.trim());
  if (!match) {
    return undefined;
  }
  const [, startRaw, endRaw] = match;
  if (!startRaw && !endRaw) {
    return undefined;
  }
  if (!startRaw) {
    // Suffix form: the last N bytes. N=0 is syntactically valid but names
    // zero bytes, so the header is ignored per RFC 7233.
    const suffixLength = Number(endRaw);
    if (!Number.isSafeInteger(suffixLength) || suffixLength === 0) {
      return undefined;
    }
    if (size === 0) {
      return "unsatisfiable";
    }
    return { start: Math.max(size - suffixLength, 0), end: size - 1 };
  }
  const start = Number(startRaw);
  if (!Number.isSafeInteger(start)) {
    return undefined;
  }
  if (start >= size) {
    return "unsatisfiable";
  }
  const end = endRaw ? Number(endRaw) : size - 1;
  if (!Number.isSafeInteger(end) || end < start) {
    return undefined;
  }
  return { start, end: Math.min(end, size - 1) };
}

export function pdfResponseHeaders(base?: HeadersInit): Headers {
  const headers = new Headers(base);
  headers.set("content-type", "application/pdf");
  headers.set("access-control-allow-origin", "*");
  headers.set("accept-ranges", "bytes");
  return headers;
}

// Serves the byte range named by the request's Range header, or returns
// undefined when the header is absent/unparseable and the caller should
// fall back to a full-body response.
export function rangedPdfResponse(request: Request, filePath: string): Response | undefined {
  const rangeHeader = request.headers.get("range");
  if (!rangeHeader) {
    return undefined;
  }
  let size: number;
  try {
    size = statSync(filePath).size;
  } catch {
    return undefined;
  }
  const range = parseByteRangeHeader(rangeHeader, size);
  if (range === undefined) {
    return undefined;
  }
  if (range === "unsatisfiable") {
    return new Response("Range not satisfiable", {
      status: 416,
      headers: pdfResponseHeaders({ "content-range": `bytes */${size}` }),
    });
  }
  const body = Readable.toWeb(
    createReadStream(filePath, { start: range.start, end: range.end }),
  ) as unknown as ReadableStream;
  return new Response(body, {
    status: 206,
    headers: pdfResponseHeaders({
      "content-range": `bytes ${range.start}-${range.end}/${size}`,
      "content-length": String(range.end - range.start + 1),
    }),
  });
}
