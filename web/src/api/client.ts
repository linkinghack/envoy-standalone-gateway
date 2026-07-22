import createClient, {type Middleware} from "openapi-fetch";
import type {paths} from "./generated";

export class ManagementAPIError extends Error {
  constructor(public readonly status: number, public readonly code: string, message: string, public readonly details?: unknown) {
    super(message);
    this.name = "ManagementAPIError";
  }
}

const csrf: Middleware = {
  onRequest({request}) {
    if (!new Set(["GET", "HEAD", "OPTIONS"]).has(request.method.toUpperCase())) {
      request.headers.set("X-ESGW-Request", "1");
    }
    return request;
  },
};

export const api = createClient<paths>({baseUrl: "/api/v1", credentials: "include"});
api.use(csrf);
export const csrfHeader = {"X-ESGW-Request": "1"} as const;

export function unwrap<T>({data, error, response}: {data?: T; error?: unknown; response: Response}): T {
  if (!response.ok || error !== undefined) throw toAPIError(response.status, error);
  return data as T;
}

export function unwrapVoid({error, response}: {error?: unknown; response: Response}) {
  if (!response.ok || error !== undefined) throw toAPIError(response.status, error);
}

export function toAPIError(status: number, payload: unknown) {
  const root = isRecord(payload) && isRecord(payload.error) ? payload.error : {};
  const code = typeof root.code === "string" ? root.code : status === 401 ? "UNAUTHENTICATED" : "REQUEST_FAILED";
  const message = typeof root.message === "string" ? root.message : status === 0 ? "The management API could not be reached." : `Request failed (${status}).`;
  return new ManagementAPIError(status, code, message, root.details);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object";
}
