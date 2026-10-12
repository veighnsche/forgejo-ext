import {vi} from 'vitest';
import {mountContribution, parseMountDataset, createScopedRequest, createScopedStream} from './extension-mount.js';

function pageRoot() {
  const root = document.createElement('div');
  root.dataset.appSubUrl = '';
  root.dataset.extensionRepositoryId = '7';
  root.dataset.extensionEntry = '/-/native/assets/example/main.js';
  root.dataset.extensionAssetBase = '/-/native/assets/example/';
  root.dataset.extensionApiBase = '/-/native/pages/example/home/api/';
  root.dataset.extensionId = 'example';
  root.dataset.extensionPageId = 'home';
  root.dataset.extensionSessionGeneration = 'session-a';
  return root;
}

class FakeSocket {
  constructor(url) {
    this.url = url;
    this.closed = false;
  }
  close() {
    this.closed = true;
  }
}

test('mounts a retained owner with scoped request and stream helpers', async () => {
  const root = pageRoot();
  const dispose = vi.fn();
  const mount = vi.fn(() => dispose);
  const importer = vi.fn(async () => ({mount}));
  const fetcher = vi.fn().mockResolvedValue({status: 200});
  const owner = await mountContribution(root, {importer, fetcher, websocketClass: FakeSocket});
  expect(owner).not.toBeNull();
  expect(owner.root).toBe(root);
  expect(importer).toHaveBeenCalledWith(`${window.location.origin}/-/native/assets/example/main.js`);
  expect(mount).toHaveBeenCalledWith(root, expect.objectContaining({
    extensionId: 'example', pageId: 'home', panelId: undefined, appSubUrl: '',
    repositoryId: '7', sessionGeneration: 'session-a',
    request: expect.any(Function), stream: expect.any(Function),
  }));
  await owner.context.request('context');
  expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/-/native/pages/example/home/api/context`, expect.objectContaining({
    credentials: 'same-origin', redirect: 'error', cache: 'no-store',
  }));
  expect(fetcher.mock.calls[0][1].headers.get('X-Extension-Session-Generation')).toBe('session-a');
  const socket = owner.context.stream('socket');
  expect(socket).toBeInstanceOf(FakeSocket);
  expect(socket.url).toBe(`${window.location.origin}/-/native/pages/example/home/api/socket`.replace(/^http/, 'ws'));
  owner.dispose();
  expect(dispose).toHaveBeenCalledOnce();
});

test('mounts a panel under an installation subpath', async () => {
  const root = document.createElement('div');
  root.dataset.appSubUrl = '/forge';
  root.dataset.extensionEntry = '/forge/-/native/assets/example/panel.js';
  root.dataset.extensionAssetBase = '/forge/-/native/assets/example/';
  root.dataset.extensionApiBase = '/forge/-/native/panels/example/notes/api/';
  root.dataset.extensionId = 'example';
  root.dataset.extensionPanelId = 'notes';
  root.dataset.extensionSessionGeneration = 'session-a';
  const mount = vi.fn();
  const owner = await mountContribution(root, {importer: vi.fn(async () => ({mount})), websocketClass: FakeSocket});
  expect(owner).not.toBeNull();
  expect(mount).toHaveBeenCalledWith(root, expect.objectContaining({
    pageId: undefined, panelId: 'notes', repositoryId: undefined,
  }));
  owner.dispose();
});

test('drives ordered update, suspend, resume, and retire', async () => {
  const root = pageRoot();
  const calls = [];
  const handle = {
    update: vi.fn((presentation) => calls.push(['update', presentation.route])),
    suspend: vi.fn(() => calls.push(['suspend'])),
    resume: vi.fn(() => calls.push(['resume'])),
    retire: vi.fn(() => calls.push(['retire'])),
    dispose: vi.fn(() => calls.push(['dispose'])),
  };
  const owner = await mountContribution(root, {importer: vi.fn(async () => ({mount: () => handle})), websocketClass: FakeSocket});
  const socket = owner.context.stream('socket');
  owner.update({mode: 'page', route: '/workspace', params: {}, visible: true});
  owner.update({mode: 'page', route: '/team/repo', params: {}, repositoryId: '7', visible: true});
  expect(handle.update).toHaveBeenCalledTimes(2);
  expect(socket.closed).toBe(false);
  owner.suspend();
  expect(socket.closed).toBe(false);
  expect(handle.suspend).toHaveBeenCalledOnce();
  owner.resume({sessionGeneration: 'session-a'});
  expect(handle.resume).toHaveBeenCalledWith({sessionGeneration: 'session-a'});
  expect(socket.closed).toBe(false);
  owner.retire('session-changed');
  expect(socket.closed).toBe(true);
  expect(handle.retire).toHaveBeenCalledWith('session-changed');
  expect(calls).toEqual([
    ['update', '/workspace'], ['update', '/team/repo'],
    ['suspend'], ['resume'], ['retire'], ['dispose'],
  ]);
  owner.update({mode: 'page', route: '/late', params: {}, visible: true});
  expect(handle.update).toHaveBeenCalledTimes(2);
});

test('refuses invalid mounts without importing or fetching', async () => {
  const cases = [
    (root) => { root.dataset.extensionEntry = '/-/native/assets/other/main.js' },
    (root) => { root.dataset.extensionApiBase = '/-/native/pages/example/other/api/' },
    (root) => { root.dataset.extensionApiBase = '/-/native/pages/other/home/api/' },
    (root) => { delete root.dataset.extensionSessionGeneration },
    (root) => { root.dataset.appSubUrl = '/wrong' },
  ];
  for (const mutate of cases) {
    const root = pageRoot();
    mutate(root);
    expect(parseMountDataset(root).ok).toBe(false);
    const importer = vi.fn(async () => ({mount: vi.fn()}));
    const fetcher = vi.fn();
    const owner = await mountContribution(root, {importer, fetcher, websocketClass: FakeSocket});
    expect(owner).toBeNull();
    expect(root.textContent).toBe('This workspace view is unavailable.');
    expect(importer).not.toHaveBeenCalled();
    expect(fetcher).not.toHaveBeenCalled();
  }
});

test('rejects out-of-scope request and stream targets', async () => {
  const root = pageRoot();
  const owner = await mountContribution(root, {importer: vi.fn(async () => ({mount: vi.fn()})), websocketClass: FakeSocket});
  expect(() => owner.context.request('../other')).toThrow('Product API URL is invalid');
  expect(() => owner.context.request('https://elsewhere.example/secret')).toThrow('Product API URL is invalid');
  expect(() => owner.context.request('/team%2frepo')).toThrow('Product API URL is invalid');
  expect(() => owner.context.stream('../other')).toThrow('Product stream URL is invalid');
  expect(() => owner.context.stream('socket?token=abc')).toThrow('Product stream URL is invalid');
  expect(() => owner.context.stream('https://elsewhere.example/socket')).toThrow('Product stream URL is invalid');
  owner.dispose();
});

test('revokes transport on retire and disposes exactly once', async () => {
  const root = pageRoot();
  const dispose = vi.fn();
  const owner = await mountContribution(root, {importer: vi.fn(async () => ({mount: () => dispose})), websocketClass: FakeSocket});
  owner.retire('authority-revoked');
  expect(dispose).toHaveBeenCalledOnce();
  expect(() => owner.context.request('context')).toThrow('Contribution transport is retired');
  expect(() => owner.context.stream('socket')).toThrow('Contribution transport is retired');
  owner.retire('authority-revoked');
  owner.dispose();
  expect(dispose).toHaveBeenCalledOnce();
});

test('reports unavailable streams without a websocket implementation', () => {
  expect(() => createScopedStream('https://forge.example/-/native/pages/example/home/api/', undefined)).toThrow('Contribution streams are unavailable');
  expect(() => createScopedRequest).not.toThrow();
});
