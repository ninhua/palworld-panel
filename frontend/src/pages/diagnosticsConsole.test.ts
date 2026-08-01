import { describe, expect, it } from 'vitest';
import {
  appendDiagnosticHistory,
  createCustomDiagnosticTemplate,
  createDiagnosticHeaderRow,
  createHTTPHistoryEntry,
  diagnosticHeaderJSONToRows,
  diagnosticHeaderRowsToRecord,
  diagnosticHistoryEquivalent,
  formatDiagnosticHeaderJSON,
  formatDiagnosticJSON,
  formatHTTPResult,
  getDiagnosticTemplates,
  normalizeDiagnosticHistory,
  parseDiagnosticAuthorization,
  parseDiagnosticJSON,
  updateDiagnosticHeaderValue,
} from './diagnosticsConsole';

const result = (body: string) => ({
  method: 'GET',
  url: 'http://127.0.0.1:8212/v1/pdapi/version',
  status: '200 OK',
  status_code: 200,
  headers: { 'Content-Type': ['application/json'] },
  body,
  truncated: false,
  duration_ms: 8,
});

describe('diagnostics console helpers', () => {
  it('recognizes Authorization schemes and adds Bearer by default', () => {
    expect(parseDiagnosticAuthorization('Bearer token-value')).toEqual({ scheme: 'Bearer', value: 'token-value' });
    expect(parseDiagnosticAuthorization('Basic abc123')).toEqual({ scheme: 'Basic', value: 'abc123' });
    expect(parseDiagnosticAuthorization('token-value')).toEqual({ scheme: 'Bearer', value: 'token-value' });
    expect(parseDiagnosticAuthorization('Token abc123')).toEqual({ scheme: 'Custom', value: 'Token abc123' });

    const row = createDiagnosticHeaderRow('Authorization', 'token-value');
    expect(diagnosticHeaderRowsToRecord([row])).toEqual({ Authorization: 'Bearer token-value' });
    expect(updateDiagnosticHeaderValue(row, 'Basic abc123')).toMatchObject({ authorization_scheme: 'Basic', value: 'abc123' });
  });

  it('parses raw header JSON into editable rows and formats it', () => {
    const rows = diagnosticHeaderJSONToRows('{"Authorization":"Bearer token","Accept":"application/json"}');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ name: 'Authorization', authorization_scheme: 'Bearer', value: 'token' });
    expect(diagnosticHeaderRowsToRecord(rows)).toEqual({ Authorization: 'Bearer token', Accept: 'application/json' });
    expect(formatDiagnosticHeaderJSON('{"Authorization":"token"}', false)).toContain('"Authorization": "Bearer token"');
    expect(formatDiagnosticHeaderJSON('{"Authorization":"token"}', true)).toBe('{"Authorization":"Bearer token"}');
  });

  it('detects and formats JSON response bodies', () => {
    const document = parseDiagnosticJSON('{"ok":true,"items":[1,2],"nested":{"name":"Pal"}}');
    expect(document).not.toBeNull();
    expect(document?.summary).toBe('对象 · 3 项');
    expect(document?.formatted).toContain('  "items": [');
    expect(document?.compact).toBe('{"ok":true,"items":[1,2],"nested":{"name":"Pal"}}');
    expect(formatDiagnosticJSON(' [1, 2, 3] ', false)).toBe('[\n  1,\n  2,\n  3\n]');
    expect(formatDiagnosticJSON('{"ok":true}', true)).toBe('{"ok":true}');
    expect(parseDiagnosticJSON('plain text')).toBeNull();
    expect(parseDiagnosticJSON('null')?.summary).toBe('null');
  });

  it('uses a formatted JSON body in the complete HTTP response text', () => {
    const formatted = formatHTTPResult(result('{"ok":true}'), '{\n  "ok": true\n}');
    expect(formatted).toContain('200 OK · 8 ms');
    expect(formatted).toContain('Content-Type: application/json');
    expect(formatted).toContain('{\n  "ok": true\n}');
  });

  it('preserves Authorization and request data in history', () => {
    const entry = createHTTPHistoryEntry(
      {
        method: 'POST',
        url: 'http://127.0.0.1:8212/test?token=abc',
        headers: { Authorization: 'Bearer secret-token', Cookie: 'session=abc' },
        body: '{"password":"secret"}',
      },
      result('{"token":"response-token"}'),
    );
    expect(entry.mode).toBe('http');
    if (entry.mode !== 'http') throw new Error('unexpected mode');
    expect(entry.request.headers.Authorization).toBe('Bearer secret-token');
    expect(entry.request.headers.Cookie).toBe('session=abc');
    expect(entry.request.body).toBe('{"password":"secret"}');
    expect(entry.result.body).toBe('{"token":"response-token"}');
  });

  it('deduplicates history by request and keeps the latest response', () => {
    const request = { method: 'GET', url: 'http://127.0.0.1:8212/version', headers: { Authorization: 'Bearer token' }, body: '' };
    const older = createHTTPHistoryEntry(request, result('old'));
    const newer = createHTTPHistoryEntry(request, result('new'));
    expect(diagnosticHistoryEquivalent(older, newer)).toBe(true);
    const entries = appendDiagnosticHistory([older], newer);
    expect(entries).toHaveLength(1);
    expect(entries[0].id).toBe(newer.id);
    expect(entries[0].mode === 'http' && entries[0].result.body).toBe('new');
    expect(normalizeDiagnosticHistory([older, newer])).toHaveLength(1);
  });

  it('saves full values in custom templates', () => {
    const template = createCustomDiagnosticTemplate('PalDefender 本机', {
      mode: 'http',
      method: 'GET',
      url: 'http://127.0.0.1:8212/v1/pdapi/version',
      headers: { Authorization: 'Bearer exact-token' },
      body: '',
    });
    expect(template.custom).toBe(true);
    expect(template.headers?.Authorization).toBe('Bearer exact-token');
  });

  it('returns platform-specific built-in templates', () => {
    expect(getDiagnosticTemplates('windows').some((item) => item.command?.includes('netstat -ano'))).toBe(true);
    expect(getDiagnosticTemplates('linux').some((item) => item.command?.includes('ss -lntp'))).toBe(true);
    expect(getDiagnosticTemplates('linux').some((item) => item.headers?.Authorization?.startsWith('Bearer '))).toBe(true);
  });
});
