import axios, { AxiosError, AxiosResponse } from 'axios';
import {
  appEvents,
} from '../config/defaults';

interface ApiEnvelope<T = unknown> {
  ok: boolean;
  data?: T | null;
  error?: {
    code?: string;
    message?: string;
  } | string;
}

interface HandleRequestOptions<R> {
  map?: (data: unknown) => R;
  quiet?: boolean;
  fallbackOnError?: boolean;
}

const fallbackDelayMs = 120;
const sameOriginApiBaseUrl = '/api';

export const currentApiBaseUrl = () => sameOriginApiBaseUrl;

export class ApiError extends Error {
  status?: number;
  code?: string;

  constructor(message: string, status?: number, code?: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export const apiClient = axios.create({
  baseURL: currentApiBaseUrl(),
  withCredentials: true,
  timeout: 8000,
  headers: {
    'Content-Type': 'application/json',
  },
});

apiClient.interceptors.request.use((config) => {
  config.baseURL = currentApiBaseUrl();
  return config;
});

const isAxiosResponse = (value: unknown): value is AxiosResponse => {
  return Boolean(value && typeof value === 'object' && 'data' in value && 'status' in value);
};

const isApiEnvelope = (value: unknown): value is ApiEnvelope => {
  return Boolean(value && typeof value === 'object' && 'ok' in value);
};

const fallbackFor = <T>(fallback: T): T => {
  return (Array.isArray(fallback) ? [] : fallback) as T;
};

export const demoDataEnabled = () => {
  return Boolean(import.meta.env.DEV && import.meta.env.VITE_ENABLE_DEMO_DATA === 'true');
};

export const unwrapApiData = <T>(response: unknown, fallback: T): T => {
  const body = isAxiosResponse(response) ? response.data : response;

  if (isApiEnvelope(body)) {
    if (!body.ok) {
      return fallbackFor(fallback);
    }
    return body.data == null ? fallbackFor(fallback) : (body.data as T);
  }

  return body == null ? fallbackFor(fallback) : (body as T);
};

const apiErrorFrom = (error: unknown): ApiError => {
  const axiosError = error as AxiosError<ApiEnvelope> | undefined;
  const status = axiosError?.response?.status;
  const payload = axiosError?.response?.data;
  if (!status && axiosError?.code === 'ERR_NETWORK') {
    return new ApiError('无法连接当前面板后端，请确认 PalPanel 服务正在运行。');
  }
  if (!status && (axiosError?.code === 'ECONNABORTED' || axiosError?.code === 'ETIMEDOUT')) {
    return new ApiError('当前面板后端响应超时，请稍后重试。');
  }
  if (status === 401 && typeof payload?.error === 'object' && ['steam_login_required', 'invalid_steam_credentials'].includes(payload.error?.code || '')) {
    return new ApiError(payload.error.message || 'Steam 登录已失效，请重新登录。', status, payload.error.code);
  }
  if (status === 401) {
    return new ApiError('登录状态已失效，请重新登录。', status, 'unauthorized');
  }
  if (payload?.error) {
    if (typeof payload.error === 'string') {
      return new ApiError(payload.error, status);
    }
    return new ApiError(payload.error.message || payload.error.code || 'API request failed', status, payload.error.code);
  }
  if (status === 502 || status === 503 || status === 504) {
    return new ApiError(
      `访问链路暂时无法连接 PalPanel 后端（HTTP ${status}）。安装或更新任务可能仍在运行，请稍后重试或刷新页面。`,
      status,
      'backend_temporarily_unavailable',
    );
  }
  if (error instanceof ApiError) {
    return error;
  }
  if (error instanceof Error) {
    return new ApiError(error.message, status);
  }
  return new ApiError('API request failed', status);
};

const getStatusCode = (error: unknown) => {
  const axiosError = error as AxiosError | undefined;
  return axiosError?.response?.status;
};

const shouldSuppressLog = (error: unknown, quiet?: boolean) => {
  if (quiet) return true;
  const statusCode = getStatusCode(error);
  if (!statusCode) return false;
  return [400, 401, 403, 404, 409, 422, 500, 502, 503, 504].includes(statusCode);
};

const logFallback = (error: unknown, quiet?: boolean) => {
  if (!import.meta.env.DEV) return;
  if (shouldSuppressLog(error, quiet)) {
    const statusCode = getStatusCode(error);
    console.debug('API request used fallback', statusCode ? `HTTP ${statusCode}` : error);
    return;
  }

  console.warn('API request failed, falling back to local data:', error);
};

const notifyAuthError = (error: unknown) => {
  const axiosError = error as AxiosError<ApiEnvelope> | undefined;
  const payloadError = axiosError?.response?.data?.error;
  const errorCode = typeof payloadError === 'object' ? payloadError?.code : undefined;
  if (getStatusCode(error) === 401 && !['steam_login_required', 'invalid_steam_credentials'].includes(errorCode || '') && typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent(appEvents.authError));
  }
};

export const handleRequest = async <T, R = T>(
  requestFn: () => Promise<unknown>,
  fallback: R,
  options: HandleRequestOptions<R> = {},
): Promise<R> => {
  const fallbackOnError = options.fallbackOnError ?? demoDataEnabled();
  try {
    const response = await requestFn();
    const body = isAxiosResponse(response) ? response.data : response;
    if (!fallbackOnError && isApiEnvelope(body) && !body.ok) {
      const error = body.error;
      if (typeof error === 'string') {
        throw new ApiError(error);
      }
      throw new ApiError(error?.message || error?.code || 'API request failed', undefined, error?.code);
    }
    const data = unwrapApiData<unknown>(response, fallback);
    const mapped = options.map ? options.map(data) : (data as R);
    return mapped == null ? fallbackFor(fallback) : mapped;
  } catch (error) {
    notifyAuthError(error);
    if (!fallbackOnError) {
      throw apiErrorFrom(error);
    }
    logFallback(error, options.quiet);
    await new Promise((resolve) => setTimeout(resolve, fallbackDelayMs));
    return fallbackFor(fallback);
  }
};


export const isTemporaryBackendError = (error: unknown): error is ApiError => {
  return error instanceof ApiError && [502, 503, 504].includes(error.status || 0);
};

export const getErrorMessage = (error: unknown, fallback = '操作失败，请检查后端状态') => {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return fallback;
};
