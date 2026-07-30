const runtimeVersion = '5.24.0';
const runtimeRoot = '/vendor/maplibre-gl';
const runtimeScriptURL = `${runtimeRoot}/maplibre-gl.js?v=${runtimeVersion}`;
const runtimeStylesheetURL = `${runtimeRoot}/maplibre-gl.css?v=${runtimeVersion}`;

export type MapLibreLngLat = [number, number];

export interface MapLibreGeoJSONSource {
  setData: (data: unknown) => void;
}

export interface MapLibreMapInstance {
  on: {
    (event: 'load' | 'remove' | 'error', listener: (event?: unknown) => void): void;
    (event: 'click' | 'mouseenter' | 'mouseleave', layerID: string, listener: (event: MapLibreLayerEvent) => void): void;
  };
  addControl: (control: unknown, position?: string) => void;
  addImage: (id: string, image: ImageData | { width: number; height: number; data: Uint8Array | Uint8ClampedArray }, options?: Record<string, unknown>) => void;
  hasImage: (id: string) => boolean;
  getCanvas: () => HTMLCanvasElement;
  getSource: (id: string) => MapLibreGeoJSONSource | undefined;
  resize: () => void;
  remove: () => void;
}

export interface MapLibreLayerEvent {
  features?: Array<{ properties?: Record<string, unknown> }>;
}

export interface MapLibreRuntimeModule {
  Map: new (options: Record<string, unknown>) => MapLibreMapInstance;
  NavigationControl: new (options?: Record<string, unknown>) => unknown;
  version?: string;
}

type MapLibreWindow = Window & { maplibregl?: unknown };

let runtimePromise: Promise<MapLibreRuntimeModule> | null = null;

export const validateMapLibreRuntime = (value: unknown): MapLibreRuntimeModule => {
  if (!value || typeof value !== 'object') throw new Error('MapLibre runtime did not initialize');
  const runtime = value as Partial<MapLibreRuntimeModule>;
  if (typeof runtime.Map !== 'function' || typeof runtime.NavigationControl !== 'function') {
    throw new Error('MapLibre runtime exports are incomplete');
  }
  if (runtime.version && runtime.version !== runtimeVersion) {
    throw new Error(`MapLibre runtime version mismatch: expected ${runtimeVersion}, received ${runtime.version}`);
  }
  return runtime as MapLibreRuntimeModule;
};

export const loadMapLibreRuntime = async (): Promise<MapLibreRuntimeModule> => {
  if (typeof document === 'undefined' || typeof window === 'undefined') {
    throw new Error('MapLibre runtime requires a browser document');
  }
  ensureMapLibreStylesheet();
  runtimePromise ??= loadMapLibreScript().catch((error: unknown) => {
    runtimePromise = null;
    throw error;
  });
  return runtimePromise;
};

const loadMapLibreScript = (): Promise<MapLibreRuntimeModule> => new Promise((resolve, reject) => {
  const runtimeWindow = window as MapLibreWindow;
  if (runtimeWindow.maplibregl) {
    try {
      resolve(validateMapLibreRuntime(runtimeWindow.maplibregl));
    } catch (error) {
      reject(error);
    }
    return;
  }

  const selector = `script[data-palpanel-maplibre="${runtimeScriptURL}"]`;
  const existing = document.querySelector<HTMLScriptElement>(selector);
  const script = existing ?? document.createElement('script');
  const finish = () => {
    try {
      resolve(validateMapLibreRuntime(runtimeWindow.maplibregl));
    } catch (error) {
      reject(error);
    }
  };
  const fail = () => reject(new Error(`MapLibre runtime failed to load: ${runtimeScriptURL}`));

  script.addEventListener('load', finish, { once: true });
  script.addEventListener('error', fail, { once: true });
  if (!existing) {
    script.src = runtimeScriptURL;
    script.async = true;
    script.dataset.palpanelMaplibre = runtimeScriptURL;
    document.head.appendChild(script);
  }
});

const ensureMapLibreStylesheet = () => {
  if (typeof document === 'undefined') return;
  if (document.querySelector(`link[data-palpanel-maplibre="${runtimeStylesheetURL}"]`)) return;
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = runtimeStylesheetURL;
  link.dataset.palpanelMaplibre = runtimeStylesheetURL;
  document.head.appendChild(link);
};
