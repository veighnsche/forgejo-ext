// Admin Extensions progress observation (RCX-2 F04, frozen
// fountain-design section 7). Refreshes the status table every two
// seconds while this page is visible and any operation is active; stops
// on hidden, unload, or terminal state. This is UI progress observation
// only, not a lifecycle retry or server scheduling system: manual page
// refresh remains sufficient without JavaScript.
import {GET} from '../modules/fetch.js';

type ExtensionStatusRow = {
  id: string;
  state: string;
  progress?: string | null;
  error?: {code: string; message: string} | null;
};

let statusTimer: number | null = null;
let statusListener: (() => void) | null = null;

function rowActive(row: HTMLTableRowElement): boolean {
  const state = row.dataset.extState ?? '';
  if (state === 'activating' || state === 'deactivating') return true;
  return row.dataset.extUnconfirmed === 'true';
}

function tableActive(table: HTMLTableElement): boolean {
  const rows = table.querySelectorAll('tr[id^="ext-row-"]');
  for (let i = 0; i < rows.length; i++) {
    if (rowActive(rows[i] as HTMLTableRowElement)) return true;
  }
  return false;
}

function applyRow(table: HTMLTableElement, status: ExtensionStatusRow): void {
  const row = table.querySelector(`#ext-row-${CSS.escape(status.id)}`);
  if (!(row instanceof HTMLTableRowElement)) return;
  row.dataset.extState = status.state;
  const stateCell = row.querySelector('.ext-state');
  if (stateCell) stateCell.textContent = status.state;
  const progressCell = row.querySelector('.ext-progress');
  if (progressCell) {
    progressCell.textContent = status.error?.message ?? status.progress ?? '';
  }
}

async function refreshStatus(table: HTMLTableElement): Promise<void> {
  if (document.hidden) return;
  const url = table.dataset.statusUrl;
  if (!url) return;
  let response: Response;
  try {
    response = await GET(url, {headers: {Accept: 'application/json'}});
  } catch {
    return;
  }
  if (!response.ok) {
    if (response.status === 403 || response.status === 404) stopAdminExtensions();
    return;
  }
  let rows: ExtensionStatusRow[];
  try {
    rows = (await response.json()) as ExtensionStatusRow[];
  } catch {
    return;
  }
  if (!Array.isArray(rows)) return;
  for (let i = 0; i < rows.length; i++) {
    const status = rows[i];
    if (typeof status?.id === 'string') applyRow(table, status);
  }
  if (!table.isConnected || !tableActive(table)) stopAdminExtensions();
}

export function initAdminExtensions(): void {
  const table = document.getElementById('ext-status-table');
  if (!(table instanceof HTMLTableElement) || statusTimer !== null) return;
  if (!tableActive(table)) return;
  statusListener = () => {
    refreshStatus(table).catch(() => undefined);
  };
  statusTimer = window.setInterval(statusListener, 2000);
  document.addEventListener('visibilitychange', statusListener);
  window.addEventListener('beforeunload', stopAdminExtensions);
}

export function retireAdminExtensions(): void {
  stopAdminExtensions();
}

function stopAdminExtensions(): void {
  if (statusTimer !== null) {
    window.clearInterval(statusTimer);
    statusTimer = null;
  }
  if (statusListener !== null) {
    document.removeEventListener('visibilitychange', statusListener);
    statusListener = null;
  }
  window.removeEventListener('beforeunload', stopAdminExtensions);
}
