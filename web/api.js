// JSON API client. Errors carry the server's Dutch message, ready to show.

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
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
  let res;
  try {
    res = await fetch(path, options);
  } catch {
    throw new ApiError("Geen verbinding met de server.", 0);
  }
  if (res.status === 401) {
    location.href = "/login";
    throw new ApiError("Niet ingelogd.", 401);
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
