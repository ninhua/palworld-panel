import { describe, expect, it } from 'vitest';
import {
  createHTTPHistoryEntry,
  getDiagnosticTemplates,
  maxDiagnosticHistoryEntries,
  normalizeDiagnosticHistory,
  sanitizeDiagnosticHeaders,
  sanitizeDiagnosticText,
  sanitizeDiagnosticURL,
} from './diagnosticsConsole';

describe('diagnostics console helpers', () => {
  it('redacts secrets before saving browser history', () => {
    expect(sanitizeDiagnosticHeaders({ Authorization: 'Bearer secret', Accept: 'application/json' })).toEqual({
      Authorization: '<已隐藏，不会保存到历史>',
      Accept: 'application/json',
    });
    expect(sanitizeDiagnosticURL('http://127.0.0.1:8212/test?token=abc&limit=1')).toContain('token=%3C%E5%B7%B2%E9%9A%90%E8%97%8F%3E');
    expect(sanitizeDiagnosticText('{"password":"abc","name":"server"}')).toBe('{"password":"<已隐藏>","name":"server"}');
  });

  it('caps valid history entries', () => {
    const entry = createHTTPHistoryEntry(
      { method: 'GET', url: 'http://127.0.0.1:17993/', headers: {}, body: '' },
      { method: 'GET', url: 'http://127.0.0.1:17993/', status: '200 OK', status_code: 200, headers: {}, body: 'ok', truncated: false, duration_ms: 1 },
    );
    const entries = Array.from({ length: maxDiagnosticHistoryEntries + 5 }, (_, index) => ({ ...entry, id: `${index}` }));
    expect(normalizeDiagnosticHistory(entries)).toHaveLength(maxDiagnosticHistoryEntries);
    expect(normalizeDiagnosticHistory([null, {}, entry])).toEqual([entry]);
  });

  it('returns platform-specific read-only templates', () => {
    expect(getDiagnosticTemplates('windows').some((item) => item.command?.includes('netstat -ano'))).toBe(true);
    expect(getDiagnosticTemplates('linux').some((item) => item.command?.includes('ss -lntp'))).toBe(true);
    expect(getDiagnosticTemplates('linux').some((item) => item.id === 'http-paldefender-version')).toBe(true);
  });
});
