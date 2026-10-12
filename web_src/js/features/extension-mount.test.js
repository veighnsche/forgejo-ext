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

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

test('mounts a retained owner with scoped request and declared stream access', async () => {
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
  // Declared consumer access: ContributionStream {socket, close}.
  const contribution = owner.context.stream('socket');
  expect(contribution.socket).toBeInstanceOf(FakeSocket);
  expect(contribution.socket.url).toBe(`${window.location.origin}/-/native/pages/example/home/api/socket`.replace(/^http/, 'ws'));
  contribution.close();
  expect(contribution.socket.closed).toBe(true);
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

test('drives ordered update, suspend, resume, and retire with exact identities', async () => {
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
  const contribution = owner.context.stream('socket');
  const socket = contribution.socket;
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
  expect(owner.root).toBe(root);
  expect(owner.context.stream).toBeTypeOf('function');
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

test('closes streams and revokes helpers when mount throws synchronously', async () => {
  const root = pageRoot();
  let captured;
  let opened;
  const mount = vi.fn((_ignored, context) => {
    captured = context;
    opened = context.stream('socket');
    throw new Error('mount exploded');
  });
  const silence = vi.spyOn(console, 'error').mockImplementation(() => {});
  try {
    const owner = await mountContribution(root, {importer: vi.fn(async () => ({mount})), websocketClass: FakeSocket});
    expect(owner).toBeNull();
    expect(root.textContent).toBe('Could not load this workspace view.');
    expect(opened.socket.closed).toBe(true);
    expect(() => captured.request('context')).toThrow('Contribution transport is retired');
    expect(() => captured.stream('socket')).toThrow('Contribution transport is retired');
  } finally {
    silence.mockRestore();
  }
});

test('closes streams and revokes helpers when mount rejects', async () => {
  const root = pageRoot();
  let captured;
  let opened;
  const mount = vi.fn(async (_ignored, context) => {
    captured = context;
    opened = context.stream('socket');
    throw new Error('async mount failed');
  });
  const silence = vi.spyOn(console, 'error').mockImplementation(() => {});
  try {
    const owner = await mountContribution(root, {importer: vi.fn(async () => ({mount})), websocketClass: FakeSocket});
    expect(owner).toBeNull();
    expect(opened.socket.closed).toBe(true);
    expect(() => captured.request('context')).toThrow('Contribution transport is retired');
    expect(() => captured.stream('later')).toThrow('Contribution transport is retired');
  } finally {
    silence.mockRestore();
  }
});

test('prevents mount after host invalidation while import is pending', async () => {
  const root = pageRoot();
  const controller = new AbortController();
  let resolveImport;
  const importer = vi.fn(() => new Promise((resolve) => { resolveImport = resolve }));
  const mount = vi.fn();
  const pending = mountContribution(root, {importer, websocketClass: FakeSocket, signal: controller.signal});
  controller.abort();
  resolveImport({mount});
  const owner = await pending;
  expect(owner).toBeNull();
  expect(mount).not.toHaveBeenCalled();
});

test('revokes pending helpers and streams when mount never settles', async () => {
  const root = pageRoot();
  const controller = new AbortController();
  let captured;
  let opened;
  let resolveMount;
  const retire = vi.fn();
  const dispose = vi.fn();
  const mount = vi.fn((_ignored, context) => {
    captured = context;
    opened = context.stream('socket');
    return new Promise((resolve) => { resolveMount = resolve });
  });
  const pending = mountContribution(root, {importer: vi.fn(async () => ({mount})), websocketClass: FakeSocket, signal: controller.signal});
  await flush();
  expect(mount).toHaveBeenCalledOnce();
  controller.abort();
  expect(opened.socket.closed).toBe(true);
  expect(() => captured.request('context')).toThrow('Contribution transport is retired');
  expect(() => captured.stream('socket')).toThrow('Contribution transport is retired');
  resolveMount({retire, dispose});
  const owner = await pending;
  expect(owner).toBeNull();
  expect(retire).toHaveBeenCalledWith('authority-revoked');
  expect(dispose).toHaveBeenCalledOnce();
});

test('retires a late-settling retire-only handle after invalidation', async () => {
  const root = pageRoot();
  const controller = new AbortController();
  let resolveMount;
  const retire = vi.fn();
  const mount = vi.fn(() => new Promise((resolve) => { resolveMount = resolve }));
  const pending = mountContribution(root, {importer: vi.fn(async () => ({mount})), websocketClass: FakeSocket, signal: controller.signal});
  await flush();
  expect(mount).toHaveBeenCalledOnce();
  controller.abort();
  resolveMount({retire});
  const owner = await pending;
  expect(owner).toBeNull();
  expect(retire).toHaveBeenCalledWith('authority-revoked');
});

test('retires the owner when the host aborts after settle', async () => {
  const root = pageRoot();
  const controller = new AbortController();
  const retire = vi.fn();
  const dispose = vi.fn();
  const owner = await mountContribution(root, {
    importer: vi.fn(async () => ({mount: () => ({retire, dispose})})),
    websocketClass: FakeSocket, signal: controller.signal,
  });
  expect(owner).not.toBeNull();
  controller.abort();
  expect(retire).toHaveBeenCalledWith('authority-revoked');
  expect(dispose).toHaveBeenCalledOnce();
  expect(() => owner.context.request('context')).toThrow('Contribution transport is retired');
});

test('reports unavailable streams without a websocket implementation', () => {
  expect(() => createScopedStream('https://forge.example/-/native/pages/example/home/api/', undefined)).toThrow('Contribution streams are unavailable');
  expect(() => createScopedRequest).not.toThrow();
});
