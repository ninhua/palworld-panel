import React, { useEffect, useState } from 'react';
import { Cat } from 'lucide-react';

const iconBase = '/assets/pals';

const palIconURL = (characterID: string) => {
  const normalized = characterID.trim().toLowerCase().replace(/[^a-z0-9_-]/g, '');
  return normalized ? `${iconBase}/${encodeURIComponent(normalized)}.png` : '';
};

export const PalIcon: React.FC<{ characterID?: string; name: string; className?: string }> = ({ characterID = '', name, className = '' }) => {
  const source = palIconURL(characterID);
  const [failed, setFailed] = useState(!source);

  useEffect(() => {
    setFailed(!source);
  }, [source]);

  return (
    <span className={`relative flex shrink-0 items-center justify-center overflow-hidden bg-slate-100 text-slate-400 ${className}`}>
      <Cat size={18} aria-hidden="true" />
      {!failed && (
        <img
          src={source}
          alt={`${name}图标`}
          loading="lazy"
          referrerPolicy="no-referrer"
          className="absolute inset-0 h-full w-full object-contain"
          onError={() => setFailed(true)}
        />
      )}
    </span>
  );
};
