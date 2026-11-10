// JSON API client. Errors carry the server's Dutch message, ready to show.

import { dropSnapshot } from "./offline.js";

// Funnel answers like this while the server is down or restarting.
const GATEWAY_ERRORS = new Set([502, 503, 504]);
// A read waits this long; then the server counts as unreachable. Writes have
// no limit: they may still be applied after the app gave up on them.
const READ_TIMEOUT = 10000; // ms

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }

  // unreachable: no answer from the server itself (no network, timeout, or
  // the server is down behind Funnel).
  get unreachable() {
    return this.status === 0 || GATEWAY_ERRORS.has(this.status);
  }
}

// api sends body (if any) as JSON and returns the decoded answer, or null
// for 204. With rawType, body is a string sent as is with that content type
// (a CSV file). With keepalive, the request survives the page closing (the
// last answer of a practice session). Logged out, it goes to the login page.
export async function api(method, path, body, rawType, keepalive = false) {
  const options = { method, headers: { Accept: "application/json" }, keepalive };
  if (body !== undefined) {
    options.headers["Content-Type"] = rawType ?? "application/json";
    options.body = rawType ? body : JSON.stringify(body);
  }
  if (method === "GET" && typeof AbortSignal.timeout === "function") {
    options.signal = AbortSignal.timeout(READ_TIMEOUT);
  }
  let res;
  try {
    res = await fetch(path, options);
  } catch {
    throw new ApiError("Geen verbinding met de server.", 0);
  }
  if (res.status === 401) {
    await dropSnapshot();
    location.href = "/login";
    throw new ApiError("Niet ingelogd.", 401);
  }
  if (GATEWAY_ERRORS.has(res.status)) {
    throw new ApiError("Geen verbinding met de server.", res.status);
  }
  if (res.status === 204) {
    return null;
  }
  let data = null;
  try {
    data = await res.json();
  } catch {
    // Not JSON (e.g. a proxy error page); fall back to the generic message.
  }
  if (!res.ok) {
    throw new ApiError(data?.error || "Er ging iets mis.", res.status);
  }
  return data;
}
