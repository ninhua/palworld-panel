import React, { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';

interface DiagnosticJSONViewerProps {
  value: unknown;
}

const jsonType = (value: unknown) => {
  if (value === null) return 'null';
  if (Array.isArray(value)) return 'array';
  return typeof value;
};

const primitiveText = (value: unknown) => {
  if (typeof value === 'string') return JSON.stringify(value);
  if (value === null) return 'null';
  return String(value);
};

const primitiveClass = (value: unknown) => {
  if (typeof value === 'string') return 'text-emerald-300';
  if (typeof value === 'number') return 'text-amber-300';
  if (typeof value === 'boolean') return 'text-violet-300';
  return 'text-slate-400';
};

const DiagnosticJSONNode: React.FC<{
  key?: string;
  name?: string;
  value: unknown;
  depth: number;
  last?: boolean;
}> = ({ name, value, depth, last = true }) => {
  const composite = Array.isArray(value) || (value !== null && typeof value === 'object');
  const [expanded, setExpanded] = useState(depth < 2);
  const entries = composite ? Object.entries(value as Record<string, unknown>) : [];
  const opening = Array.isArray(value) ? '[' : '{';
  const closing = Array.isArray(value) ? ']' : '}';
  const label = name == null ? '' : `${JSON.stringify(name)}: `;

  if (!composite) {
    return (
      <div className="flex min-w-0 items-start font-mono text-xs leading-6" style={{ paddingLeft: `${depth * 1.1}rem` }}>
        {name != null && <span className="shrink-0 text-sky-300">{label}</span>}
        <span className={`min-w-0 break-all ${primitiveClass(value)}`}>{primitiveText(value)}</span>
        {!last && <span className="text-slate-500">,</span>}
        <span className="ml-2 select-none text-[10px] text-slate-600">{jsonType(value)}</span>
      </div>
    );
  }

  return (
    <div>
      <button
        type="button"
        className="flex min-w-0 items-center text-left font-mono text-xs leading-6 hover:text-white"
        style={{ paddingLeft: `${depth * 1.1}rem` }}
        onClick={() => setExpanded((current) => !current)}
        aria-expanded={expanded}
      >
        {expanded ? <ChevronDown size={13} className="mr-1 shrink-0 text-slate-500" /> : <ChevronRight size={13} className="mr-1 shrink-0 text-slate-500" />}
        {name != null && <span className="shrink-0 text-sky-300">{label}</span>}
        <span className="text-slate-300">{opening}</span>
        {!expanded && <span className="mx-1 text-slate-500">{entries.length} 项</span>}
        {!expanded && <span className="text-slate-300">{closing}</span>}
        {!expanded && !last && <span className="text-slate-500">,</span>}
      </button>
      {expanded && (
        <>
          {entries.length === 0 && (
            <div className="font-mono text-xs leading-6 text-slate-600" style={{ paddingLeft: `${(depth + 1) * 1.1}rem` }}>(空)</div>
          )}
          {entries.map(([key, item], index) => (
            <DiagnosticJSONNode
              key={`${depth}:${key}`}
              name={Array.isArray(value) ? String(index) : key}
              value={item}
              depth={depth + 1}
              last={index === entries.length - 1}
            />
          ))}
          <div className="font-mono text-xs leading-6 text-slate-300" style={{ paddingLeft: `${depth * 1.1 + 1.05}rem` }}>
            {closing}{!last ? ',' : ''}
          </div>
        </>
      )}
    </div>
  );
};

export const DiagnosticJSONViewer: React.FC<DiagnosticJSONViewerProps> = ({ value }) => (
  <div className="overflow-auto rounded-2xl border border-slate-800 bg-slate-900 p-4">
    <DiagnosticJSONNode value={value} depth={0} />
  </div>
);
