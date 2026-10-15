// Admin Extensions observes pending work only. Structural changes reload the
// server-rendered page so Settings, actions and CAS pins share one owner.
import {GET} from '../modules/fetch.js';

type ExtensionStatusRow = {
  id: string;
  state: string;
  generation: string;
  installed: boolean;
  progress?: string | null;
  error?: {code: string; message: string} | null;
};

let retire: (() => void) | null = null;

export function initAdminExtensions(): void {
  const table = document.getElementById('ext-status-table');
  if (!(table instanceof HTMLTableElement) || retire) return;
  if (!table.querySelector('[data-ext-state="activating"], [data-ext-state="deactivating"]')) return;
  let timer: number | null = null;
  let request: AbortController | null = null;
  let retired = false;

  function pause(): void {
    if (timer !== null) window.clearInterval(timer);
    timer = null;
    request?.abort();
    request = null;
  }

  function stop(): void {
    retired = true;
    pause();
    document.removeEventListener('visibilitychange', visibility);
    window.removeEventListener('beforeunload', stop);
    retire = null;
  }

  async function refresh(): Promise<void> {
    if (retired || document.hidden || request) return;
    const url = table.dataset.statusUrl;
    if (!url || !table.isConnected) { stop(); return }
    const controller = new AbortController();
    request = controller;
    try {
      const response = await GET(url, {headers: {Accept: 'application/json'}, signal: controller.signal});
      if (controller.signal.aborted || retired) return;
      if (!response.ok) {
        if ([401, 403, 404].includes(response.status)) stop();
        return;
      }
      const rows: ExtensionStatusRow[] = await response.json();
      if (controller.signal.aborted || retired || document.hidden || !table.isConnected) return;
      if (!Array.isArray(rows) || !rows.every((row) => typeof row?.id === 'string' && typeof row.state === 'string' && typeof row.generation === 'string' && typeof row.installed === 'boolean')) return;
      const current = table.querySelectorAll<HTMLTableRowElement>('tr[id^="ext-row-"]');
      const changed = rows.length !== current.length || rows.some((status) => {
        const row = table.querySelector<HTMLTableRowElement>(`#ext-row-${CSS.escape(status.id)}`);
        return !row || row.dataset.extState !== status.state || row.dataset.extGeneration !== status.generation || row.dataset.extInstalled !== String(status.installed);
      });
      if (changed) {
        stop();
        window.location.reload();
        return;
      }
      for (const status of rows) {
        const progress = table.querySelector(`#ext-row-${CSS.escape(status.id)} .ext-progress`);
        if (progress) progress.textContent = status.error?.message ?? status.progress ?? '';
      }
    } catch {
      // A manual refresh remains sufficient if observation is interrupted.
    } finally {
      if (request === controller) request = null;
    }
  }

  function visibility(): void {
    if (document.hidden) { pause(); return }
    if (timer === null) timer = window.setInterval(() => { refresh().catch(() => undefined) }, 2000);
    refresh().catch(() => undefined);
  }

  retire = stop;
  document.addEventListener('visibilitychange', visibility);
  window.addEventListener('beforeunload', stop);
  if (!document.hidden) timer = window.setInterval(() => { refresh().catch(() => undefined) }, 2000);
}

export function retireAdminExtensions(): void {
  retire?.();
}
