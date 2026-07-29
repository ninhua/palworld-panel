const runtimeRoot = '/vendor/maplibre-gl';
const runtimeModuleURL = `${runtimeRoot}/maplibre-gl.mjs`;
const runtimeStylesheetURL = `${runtimeRoot}/maplibre-gl.css`;

export type MapLibreLngLat = [number, number];

export interface MapLibreGeoJSONSource {
  setData: (data: unknown) => void;
}

export interface MapLibreMapInstance {
  on: {
    (event: 'load' | 'remove', listener: () => void): void;
    (event: 'click' | 'mouseenter' | 'mouseleave', layerID: string, listener: (event: MapLibreLayerEvent) => void): void;
  };
  addControl: (control: unknown, position?: string) => void;
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

let runtimePromise: Promise<MapLibreRuntimeModule> | null = null;

export const loadMapLibreRuntime = async (): Promise<MapLibreRuntimeModule> => {
  ensureMapLibreStylesheet();
  runtimePromise ??= import(/* @vite-ignore */ runtimeModuleURL).then((value: unknown) => {
    if (!value || typeof value !== 'object') throw new Error('MapLibre runtime did not return a module');
    const runtime = value as Partial<MapLibreRuntimeModule>;
    if (typeof runtime.Map !== 'function' || typeof runtime.NavigationControl !== 'function') {
      throw new Error('MapLibre runtime exports are incomplete');
    }
    return runtime as MapLibreRuntimeModule;
  }).catch((error: unknown) => {
    runtimePromise = null;
    throw error;
  });
  return runtimePromise;
};

const ensureMapLibreStylesheet = () => {
  if (typeof document === 'undefined') return;
  if (document.querySelector(`link[data-palpanel-maplibre="${runtimeStylesheetURL}"]`)) return;
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = runtimeStylesheetURL;
  link.dataset.palpanelMaplibre = runtimeStylesheetURL;
  document.head.appendChild(link);
};
