export class ApiError extends Error {
  constructor(message: string, public readonly status: number) {
    super(message.trim() || `请求失败 (${status})`);
  }
}

export async function requestJson<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  const response = await fetch(path, { ...init, headers });
  if (!response.ok) throw new ApiError(await response.text(), response.status);
  if (response.status === 204) return null as T;
  return response.json() as Promise<T>;
}

export const jsonBody = (value: unknown) => JSON.stringify(value);
