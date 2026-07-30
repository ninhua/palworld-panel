import { apiClient, handleRequest } from './client';

export interface PatchInfo {
  patch: {
    version: string;
    repository: string;
    features: string[];
  };
  compatibility: {
    target_version: string;
    verified: boolean;
  };
  build: {
    version?: string;
    commit?: string;
    build_time?: string;
  };
}

const fallback: PatchInfo = {
  patch: { version: '', repository: '', features: [] },
  compatibility: { target_version: '', verified: false },
  build: {},
};

const asRecord = (value: unknown): Record<string, unknown> => (
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
);

export const mapPatchInfo = (value: unknown): PatchInfo => {
  const data = asRecord(value);
  const patch = asRecord(data.patch);
  const compatibility = asRecord(data.compatibility);
  const build = asRecord(data.build);
  return {
    patch: {
      version: String(patch.version || ''),
      repository: String(patch.repository || ''),
      features: Array.isArray(patch.features) ? patch.features.map(String) : [],
    },
    compatibility: {
      target_version: String(compatibility.target_version || ''),
      verified: Boolean(compatibility.verified),
    },
    build: {
      version: build.version ? String(build.version) : undefined,
      commit: build.commit ? String(build.commit) : undefined,
      build_time: build.build_time ? String(build.build_time) : undefined,
    },
  };
};

export const patchInfoApi = {
  get: () => handleRequest<unknown, PatchInfo>(
    () => apiClient.get('/patch/info'),
    fallback,
    { map: mapPatchInfo, quiet: true, fallbackOnError: false },
  ),
};
