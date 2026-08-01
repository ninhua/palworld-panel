import React, { useEffect, useMemo, useState } from 'react';
import { CRS, divIcon, type DragEndEvent, type LeafletMouseEvent, type Marker as LeafletMarker } from 'leaflet';
import { ImageOverlay, MapContainer, Marker, Polygon, Polyline, useMap, useMapEvents } from 'react-leaflet';
import 'leaflet/dist/leaflet.css';
import {
  MAP_DEFINITIONS,
  mapToWorldPoint,
  worldToMapPoint,
  type PalZonesMapID,
  type PalZonesPoint,
  type PalZonesZone,
} from './model';

interface Props {
  mapID: PalZonesMapID;
  zones: PalZonesZone[];
  selectedZoneID: string | null;
  invalidZoneIDs: Set<string>;
  onCreateZone: (points: PalZonesPoint[]) => void;
  onSelectZone: (zoneID: string) => void;
  onMovePoint: (zoneID: string, pointIndex: number, point: PalZonesPoint) => void;
}

const mapBounds: [[number, number], [number, number]] = [[0, 0], [-256, 256]];
const vertexIcon = divIcon({ className: 'palzones-vertex-marker', iconSize: [14, 14], iconAnchor: [7, 7] });

const DrawController: React.FC<{ mapID: PalZonesMapID; onCreateZone: (points: PalZonesPoint[]) => void }> = ({ mapID, onCreateZone }) => {
  const [drawing, setDrawing] = useState<Array<{ lat: number; lng: number }>>([]);
  const map = useMapEvents({
    click(event: LeafletMouseEvent) {
      if (!event.originalEvent.shiftKey) return;
      setDrawing((current) => [...current, { lat: event.latlng.lat, lng: event.latlng.lng }]);
    },
    dblclick(event: LeafletMouseEvent) {
      if (drawing.length < 2) return;
      const finish = [...drawing, { lat: event.latlng.lat, lng: event.latlng.lng }];
      onCreateZone(finish.map((point) => mapToWorldPoint(point, mapID)));
      setDrawing([]);
    },
  });
  useEffect(() => { map.doubleClickZoom.disable(); }, [map]);
  return <>{drawing.length > 0 && <Polyline positions={drawing.map((point) => [point.lat, point.lng])} pathOptions={{ color: '#38bdf8', weight: 2, dashArray: '6 6' }} />}</>;
};

const FocusZone: React.FC<{ mapID: PalZonesMapID; zone?: PalZonesZone }> = ({ mapID, zone }) => {
  const map = useMap();
  useEffect(() => {
    if (!zone || zone.points.length < 3) return;
    map.fitBounds(zone.points.map((point) => {
      const mapped = worldToMapPoint(point, mapID);
      return [mapped.lat, mapped.lng] as [number, number];
    }), { padding: [36, 36], maxZoom: 2 });
  }, [map, mapID, zone]);
  return null;
};

export const PalZonesMapCanvas: React.FC<Props> = ({ mapID, zones, selectedZoneID, invalidZoneIDs, onCreateZone, onSelectZone, onMovePoint }) => {
  const mapDefinition = MAP_DEFINITIONS.find((item) => item.id === mapID) ?? MAP_DEFINITIONS[0];
  const selected = zones.find((zone) => zone.id === selectedZoneID);
  const positions = useMemo(() => new Map(zones.map((zone) => [zone.id, zone.points.map((point) => {
    const mapped = worldToMapPoint(point, mapID);
    return [mapped.lat, mapped.lng] as [number, number];
  })])), [mapID, zones]);
  return (
    <div className={'relative h-[520px] min-h-[420px] overflow-hidden bg-slate-900 lg:h-[650px]'} data-testid={'palzones-map'}>
      <MapContainer key={mapID} crs={CRS.Simple} bounds={mapBounds} maxBounds={mapBounds} minZoom={-1} maxZoom={4} scrollWheelZoom attributionControl={false} zoomControl>
        <ImageOverlay url={mapDefinition.image} bounds={mapBounds} />
        {zones.map((zone) => <Polygon
          key={zone.id}
          positions={positions.get(zone.id) ?? []}
          pathOptions={{ color: invalidZoneIDs.has(zone.id) ? '#ef4444' : zone.id === selectedZoneID ? '#f59e0b' : '#0ea5e9', weight: zone.id === selectedZoneID ? 3 : 2, fillOpacity: 0.22 }}
          eventHandlers={{ click: () => onSelectZone(zone.id) }}
        />)}
        {selected?.points.map((point, pointIndex) => {
          const mapped = worldToMapPoint(point, mapID);
          return <Marker
            key={selected.id + '-' + pointIndex}
            position={[mapped.lat, mapped.lng]}
            icon={vertexIcon}
            draggable
            eventHandlers={{ dragend: (event: DragEndEvent) => {
              const marker = event.target as LeafletMarker;
              const next = marker.getLatLng();
              onMovePoint(selected.id, pointIndex, mapToWorldPoint({ lat: next.lat, lng: next.lng }, mapID));
            } }}
          />;
        })}
        <DrawController mapID={mapID} onCreateZone={onCreateZone} />
        <FocusZone mapID={mapID} zone={selected} />
      </MapContainer>
      <div className={'pointer-events-none absolute bottom-3 left-3 z-[500] bg-slate-950/85 px-3 py-2 text-[11px] font-bold text-white'}>按住 Shift 单击添加顶点，双击完成区域</div>
    </div>
  );
};
