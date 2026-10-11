import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {classifyWorkspaceNavigation, workspacePath, workspaceDirectPath, mountExtension, initExtensionPages, initExtensionWorkspace} from './extensions.js';

const origin = 'https://forge.example';
const base = `${origin}/team/repo`;

test('repository product tabs precede native unit tabs in the overflow menu', () => {
  const template = readFileSync('templates/repo/header.tmpl', 'utf8');
  const productTabs = template.indexOf('{{with .ExtensionPages}}{{range index . "repository"}}');
  const codeTab = template.indexOf('{{if .Permission.CanRead $.UnitTypeCode}}', template.indexOf('<overflow-menu'));
  expect(productTabs).toBeGreaterThan(template.indexOf('<overflow-menu'));
  expect(productTabs).toBeLessThan(codeTab);
  expect(template.match(/range index . "repository"/g)).toHaveLength(1);
  expect(template.slice(productTabs, codeTab)).toContain('{{if ne .Permission "admin"}}');
});

test('admits native same-origin paths and preserves deep links', () => {
  expect(classifyWorkspaceNavigation('/team/repo/issues/7?sort=oldest#comment-1', base, origin)).toEqual({
    kind: 'frame',
    url: `${origin}/team/repo/issues/7?sort=oldest#comment-1`,
    path: '/team/repo/issues/7?sort=oldest#comment-1',
  });
  expect(workspacePath('/team/repo/issues/7', base, origin)).toBe('/team/repo/issues/7');
  expect(workspacePath('/workspace', base, origin)).toBe('/workspace');
  expect(workspacePath('/team/repo/workspace', base, origin)).toBe('/team/repo/workspace');
  expect(workspacePath('/workspace/new', base, origin)).toBe('/workspace/new');
  expect(workspacePath('/workspace/requests/d0123456789abcdef01234567', base, origin)).toBe('/workspace/requests/d0123456789abcdef01234567');
});

test('classifies the workspace root and explicit aliases as direct', () => {
  for (const [pathname, sub] of [['/workspace', ''], ['/team/repo/workspace', ''], ['/forge/workspace', '/forge'],
    ['/workspace/new', ''], ['/forge/workspace/new', '/forge'],
    ['/workspace/requests/d0123456789abcdef01234567', ''], ['/forge/workspace/requests/d0123456789abcdef01234567', '/forge']]) {
    expect(workspaceDirectPath(pathname, sub)).toBe(true);
  }
  for (const [pathname, sub] of [['/workspace/requests', ''], ['/workspace/requests/', ''],
    ['/workspace/requests/bad', ''], ['/workspace/requests/d0123456789abcdef0123456', ''],
    ['/workspace/requests/d0123456789ABCDEF01234567', ''], ['/workspace/requests/d0123456789abcdef01234567/extra', ''],
    ['/workspace/new/extra', ''], ['/workspace/news', ''], ['/workspace/old', ''],
    ['/user2/repo1/workspace/new', ''], ['/user2/repo1/workspace/requests/d0123456789abcdef01234567', ''],
    ['/workspace/new', '/forge'], ['/other/workspace/new', '/forge'], ['/forgefoo/workspace/new', '/forge'], ['/issues', '']]) {
    expect(workspaceDirectPath(pathname, sub)).toBe(false);
  }
});

test('keeps auth and external destinations out of the frame', () => {
  expect(classifyWorkspaceNavigation('/user/login?redirect_to=%2Fteam', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/logout', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/activate?code=abc', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/two_factor', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/webauthn', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/recover_account', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/settings/security/two_factor/enroll', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/user/settings/applications', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/oauth2/authorize', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/login/openid', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/-/native/session', base, origin).kind).toBe('reject');
  expect(classifyWorkspaceNavigation('/repo?access_token=secret', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('/repo?monkey=usual', base, origin).kind).toBe('frame');
  expect(classifyWorkspaceNavigation('https://elsewhere.example/', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('data:text/html,unsafe', base, origin).kind).toBe('reject');
  expect(workspacePath('//elsewhere.example/', base, origin)).toBeNull();
  expect(workspacePath('/team%2frepo', base, origin)).toBeNull();
  expect(workspacePath('/team%5crepo', base, origin)).toBeNull();
  expect(workspacePath('/elsewhere', `${origin}/forge/team/repo`, origin, '/forge')).toBeNull();
  expect(workspacePath('/forge/team', `${origin}/forge/team/repo`, origin, '/forge')).toBe('/forge/team');
});

test('mounts an extension with explicit context and disposes its lifecycle', async () => {
  const root = document.createElement('div');
  root.dataset.appSubUrl = '';
  root.dataset.extensionRepositoryId = '7';
  root.dataset.extensionEntry = '/-/native/assets/example/main.js';
  root.dataset.extensionAssetBase = '/-/native/assets/example/';
  root.dataset.extensionApiBase = '/-/native/pages/example/home/api/';
  root.dataset.extensionId = 'example';
  root.dataset.extensionPageId = 'home';
  root.dataset.extensionSessionGeneration = 'session-a';
  const dispose = vi.fn();
  const mount = vi.fn(() => dispose);
  const importer = vi.fn(async () => ({mount}));
  const unmount = await mountExtension(root, importer);
  expect(importer).toHaveBeenCalledOnce();
  expect(mount).toHaveBeenCalledWith(root, expect.objectContaining({
    extensionId: 'example', pageId: 'home', panelId: undefined, appSubUrl: '', repositoryId: '7',
    apiBase: '/-/native/pages/example/home/api/', assetBase: '/-/native/assets/example/',
    sessionGeneration: 'session-a', request: expect.any(Function),
  }));
  const oldFetch = window.fetch;
  window.fetch = vi.fn().mockResolvedValue({status: 200});
  try {
    await mount.mock.calls[0][1].request('context', {headers: {'Content-Type': 'application/json'}});
    expect(window.fetch).toHaveBeenCalledWith(`${window.location.origin}/-/native/pages/example/home/api/context`, expect.objectContaining({
      credentials: 'same-origin', redirect: 'error', cache: 'no-store',
      headers: expect.any(Headers),
    }));
    expect(window.fetch.mock.calls[0][1].headers.get('X-Extension-Session-Generation')).toBe('session-a');
    expect(window.fetch.mock.calls[0][1].headers.get('Content-Type')).toBe('application/json');
    expect(() => mount.mock.calls[0][1].request('../other')).toThrow('Product API URL is invalid');
  } finally {
    window.fetch = oldFetch;
  }
  unmount();
  expect(dispose).toHaveBeenCalledOnce();
});

test('uses the explicit installation prefix for mounted product views and refuses mismatched metadata', async () => {
  const root = document.createElement('div');
  root.dataset.appSubUrl = '/forge';
  root.dataset.extensionEntry = '/forge/-/native/assets/example/home.js';
  root.dataset.extensionAssetBase = '/forge/-/native/assets/example/';
  root.dataset.extensionApiBase = '/forge/-/native/pages/example/home/api/';
  root.dataset.extensionId = 'example';
  root.dataset.extensionPageId = 'home';
  root.dataset.extensionSessionGeneration = 'session-a';
  const mount = vi.fn();
  const importer = vi.fn(async () => ({mount}));
  await mountExtension(root, importer);
  expect(mount).toHaveBeenCalledWith(root, expect.objectContaining({appSubUrl: '/forge', repositoryId: undefined}));
  expect(() => mount.mock.calls[0][1].request('/-/native/pages/example/home/api/context')).toThrow('Product API URL is invalid');

  root.dataset.appSubUrl = '/wrong';
  await mountExtension(root, importer);
  expect(importer).toHaveBeenCalledOnce();
  expect(root.textContent).toBe('This workspace view is unavailable.');
});

test.each(['/user/settings', '/admin', '/team/repo'])('mounts the native %s scoped API with root assets', async (scope) => {
  const root = document.createElement('div');
  root.dataset.appSubUrl = '/forge';
  root.dataset.extensionEntry = '/forge/-/native/assets/example/home.js';
  root.dataset.extensionAssetBase = '/forge/-/native/assets/example/';
  root.dataset.extensionApiBase = `/forge${scope}/-/native/pages/example/home/api/`;
  root.dataset.extensionId = 'example';
  root.dataset.extensionPageId = 'home';
  root.dataset.extensionSessionGeneration = 'session-a';
  const mount = vi.fn();
  const importer = vi.fn(async () => ({mount}));
  await mountExtension(root, importer);
  expect(mount).toHaveBeenCalledOnce();
  const oldFetch = window.fetch;
  window.fetch = vi.fn().mockResolvedValue({status: 200});
  try {
    await mount.mock.calls[0][1].request('context');
    expect(window.fetch.mock.calls[0][0]).toBe(`${window.location.origin}/forge${scope}/-/native/pages/example/home/api/context`);
    expect(window.fetch.mock.calls[0][1].headers.get('X-Extension-Session-Generation')).toBe('session-a');
    expect(() => mount.mock.calls[0][1].request('../other')).toThrow('Product API URL is invalid');
  } finally {
    window.fetch = oldFetch;
  }

  root.dataset.extensionApiBase = '/forge/team/repo/settings/-/native/pages/example/home/api/';
  await mountExtension(root, importer);
  root.dataset.extensionApiBase = `/forge${scope}/-/native/pages/example/other/api/`;
  await mountExtension(root, importer);
  expect(importer).toHaveBeenCalledOnce();
});

test('keeps a mounted panel alive when its page enters BFCache', async () => {
  document.body.innerHTML = '<div data-extension-panel data-app-sub-url="" data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/native/assets/example/panel.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/panels/example/notes/api/" data-extension-session-generation="session-a"></div>';
  const dispose = vi.fn();
  initExtensionPages(async () => ({mount: () => dispose}));
  await new Promise((resolve) => setTimeout(resolve, 0));
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  expect(dispose).not.toHaveBeenCalled();
  const final = new Event('pagehide');
  Object.defineProperty(final, 'persisted', {value: false});
  window.dispatchEvent(final);
  expect(dispose).toHaveBeenCalledOnce();
});

test('removes a cached extension page when the native session changes', async () => {
  document.body.innerHTML = '<div data-extension-page data-app-sub-url="" data-extension-id="example" data-extension-page-id="home" data-extension-entry="/-/native/assets/example/page.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/pages/example/home/api/" data-extension-session-generation="session-a">private page</div>';
  const root = document.querySelector('[data-extension-page]');
  const dispose = vi.fn();
  const fetcher = vi.fn().mockResolvedValue({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-b'})});
  initExtensionPages(async () => ({mount: () => dispose}), fetcher);
  await new Promise((resolve) => setTimeout(resolve, 0));
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  expect(root.style.display).toBe('none');
  const restored = new Event('pageshow');
  Object.defineProperty(restored, 'persisted', {value: true});
  window.dispatchEvent(restored);
  await vi.waitFor(() => expect(root.textContent).toContain('Your session changed'));
  expect(root.textContent).not.toContain('private page');
  expect(dispose).toHaveBeenCalledOnce();
  expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/-/native/session`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  });
  window.dispatchEvent(new Event('pagehide'));
});

test('workspace keeps panels and native address navigation without a duplicate topbar', () => {
  document.body.innerHTML = '<main data-extension-workspace data-app-sub-url="" data-workspace-path="/team/repo?sort=oldest&amp;page=2" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe><div data-extension-panel></div></main>';
  window.history.replaceState({nativeState: 'retained'}, '', '/team/repo?sort=oldest&page=2#comment-1');
  const frame = document.querySelector('iframe');
  let frameSource = '';
  let childURL = `${window.location.origin}/team/repo`;
  const childDocument = document.implementation.createHTMLDocument('frame');
  document.title = 'Original native page';
  Object.defineProperty(frame, 'src', {
    get: () => frameSource,
    set: (value) => { frameSource = new URL(value, window.location.href).href },
  });
  const childWindow = new EventTarget();
  Object.defineProperty(childWindow, 'location', {get: () => ({href: childURL})});
  Object.defineProperty(frame, 'contentWindow', {get: () => childWindow});
  Object.defineProperty(frame, 'contentDocument', {get: () => childDocument});
  const pushState = vi.spyOn(window.history, 'pushState');
  initExtensionWorkspace();
  expect(frameSource).toBe(`${window.location.origin}/team/repo?sort=oldest&page=2#comment-1`);
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/team/repo?sort=oldest&page=2#comment-1');
  const panel = document.querySelector('[data-extension-panel]');
  childURL = `${window.location.origin}/team/repo/issues?sort=oldest&sort=newest#comment-2`;
  childDocument.title = 'Issues - team/repo';
  frame.dispatchEvent(new Event('load'));
  expect(document.title).toBe('Issues - team/repo');
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/team/repo/issues?sort=oldest&sort=newest#comment-2');
  expect(window.history.state.nativeState).toBe('retained');
  expect(pushState).not.toHaveBeenCalled();
  expect(document.querySelector('[data-extension-panel]')).toBe(panel);
  window.history.replaceState({nativeState: 'retained'}, '', '/team/repo?sort=oldest&page=2#comment-1');
  window.dispatchEvent(new PopStateEvent('popstate'));
  expect(frameSource).toBe(`${window.location.origin}/team/repo?sort=oldest&page=2#comment-1`);
  pushState.mockRestore();
  window.dispatchEvent(new Event('pagehide'));
  childURL = `${window.location.origin}/team/repo/issues/99`;
  childDocument.title = 'Detached native page';
  frame.dispatchEvent(new Event('load'));
  childWindow.dispatchEvent(new Event('hashchange'));
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/team/repo?sort=oldest&page=2#comment-1');
  expect(document.title).toBe('Issues - team/repo');
});

test('workspace leaves modified links and downloads to the frame and routes excluded forms at top level', () => {
  document.body.innerHTML = '<main data-extension-workspace data-app-sub-url="" data-workspace-path="/team" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe></main>';
  window.history.replaceState(null, '', '/team');
  const frame = document.querySelector('iframe');
  Object.defineProperty(frame, 'src', {get: () => '', set: () => {}});
  const childDocument = document.implementation.createHTMLDocument('frame');
  const childWindow = new EventTarget();
  Object.defineProperty(childWindow, 'location', {value: {href: `${window.location.origin}/team`}});
  Object.defineProperty(frame, 'contentWindow', {value: childWindow});
  Object.defineProperty(frame, 'contentDocument', {value: childDocument});
  initExtensionWorkspace();
  frame.dispatchEvent(new Event('load'));

  const blocked = childDocument.createElement('a');
  blocked.href = '/-/native/session';
  childDocument.body.append(blocked);
  const modified = new MouseEvent('click', {bubbles: true, cancelable: true, ctrlKey: true});
  blocked.dispatchEvent(modified);
  expect(modified.defaultPrevented).toBe(false);
  blocked.download = 'file';
  const download = new MouseEvent('click', {bubbles: true, cancelable: true});
  blocked.dispatchEvent(download);
  expect(download.defaultPrevented).toBe(false);
  blocked.removeAttribute('download');
  const plain = new MouseEvent('click', {bubbles: true, cancelable: true});
  blocked.dispatchEvent(plain);
  expect(plain.defaultPrevented).toBe(true);

  const form = childDocument.createElement('form');
  form.action = '/user/settings/security/two_factor/enroll';
  childDocument.body.append(form);
  form.dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}));
  expect(form.target).toBe('_top');
  window.dispatchEvent(new Event('pagehide'));
});

test('workspace uses its declared subpath for session checks while retaining the native deep link', async () => {
  document.body.innerHTML = '<main data-extension-workspace data-app-sub-url="/forge" data-workspace-path="/forge/team/repo/issues/7?sort=oldest" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe></main>';
  window.history.replaceState(null, '', '/forge/team/repo/issues/7?sort=oldest#comment-1');
  const frame = document.querySelector('iframe');
  let frameSource;
  Object.defineProperty(frame, 'src', {get: () => frameSource, set: (value) => { frameSource = value }});
  const fetcher = vi.fn().mockResolvedValue({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  initExtensionWorkspace(fetcher);
  expect(frameSource).toBe('/forge/team/repo/issues/7?sort=oldest#comment-1');
  window.dispatchEvent(new Event('focus'));
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/forge/-/native/session`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  }));
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/forge/team/repo/issues/7?sort=oldest#comment-1');
  window.dispatchEvent(new Event('pagehide'));
});

test('hides a restored workspace until the live native generation matches and clears it on account change', async () => {
  document.body.innerHTML = '<main data-extension-workspace data-app-sub-url="" data-workspace-path="/team" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe><div data-extension-panel data-app-sub-url="" data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/native/assets/example/panel.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/panels/example/notes/api/" data-extension-session-generation="session-a"></div></main>';
  window.history.replaceState(null, '', '/team');
  const workspace = document.querySelector('[data-extension-workspace]');
  const frame = workspace.querySelector('iframe');
  let frameSource;
  Object.defineProperty(frame, 'src', {get: () => frameSource, set: (value) => { frameSource = value }});
  const dispose = vi.fn();
  initExtensionPages(async () => ({mount: () => dispose}));
  await new Promise((resolve) => setTimeout(resolve, 0));
  const fetcher = vi.fn().mockResolvedValueOnce({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})})
    .mockResolvedValueOnce({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-b'})});
  initExtensionWorkspace(fetcher);
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  expect(workspace.style.display).toBe('none');
  const restored = new Event('pageshow');
  Object.defineProperty(restored, 'persisted', {value: true});
  window.dispatchEvent(restored);
  expect(workspace.style.display).toBe('none');
  await vi.waitFor(() => expect(workspace.style.display).toBe(''));
  expect(dispose).not.toHaveBeenCalled();
  expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/-/native/session`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  });
  window.dispatchEvent(new Event('focus'));
  expect(workspace.style.display).toBe('none');
  await vi.waitFor(() => expect(workspace.textContent).toContain('Your session changed'));
  expect(frameSource).toBe('about:blank');
  expect(workspace.querySelector('[data-extension-panel]')).toBeNull();
  expect(dispose).toHaveBeenCalledOnce();
  window.dispatchEvent(new Event('pagehide'));
});

test('ignores a session check completed after the workspace was cached', async () => {
  document.body.innerHTML = '<main data-extension-workspace data-app-sub-url="" data-workspace-path="/team" data-workspace-session-generation="session-a"><iframe data-extension-workspace-frame></iframe></main>';
  window.history.replaceState(null, '', '/team');
  const workspace = document.querySelector('[data-extension-workspace]');
  const frame = workspace.querySelector('iframe');
  Object.defineProperty(frame, 'src', {get: () => '', set: () => {}});
  let resolveOld;
  let resolveCurrent;
  const fetcher = vi.fn().mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve }))
    .mockImplementationOnce(() => new Promise((resolve) => { resolveCurrent = resolve }));
  initExtensionWorkspace(fetcher);
  window.dispatchEvent(new Event('focus'));
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  const cached = new Event('pagehide');
  Object.defineProperty(cached, 'persisted', {value: true});
  window.dispatchEvent(cached);
  const restored = new Event('pageshow');
  Object.defineProperty(restored, 'persisted', {value: true});
  window.dispatchEvent(restored);
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  resolveOld({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-b'})});
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(workspace.style.display).toBe('none');
  expect(workspace.querySelector('iframe')).toBe(frame);
  resolveCurrent({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  await vi.waitFor(() => expect(workspace.style.display).toBe(''));
  window.dispatchEvent(new Event('pagehide'));
});

test('direct Workspace uses native content and chrome while retaining contribution roots', async () => {
  window.history.replaceState(null, '', '/forge/workspace?view=projects#notes');
  document.body.innerHTML = '<nav id="navbar"><a href="/forge/user/settings">Account</a></nav><main data-extension-workspace data-workspace-direct data-app-sub-url="/forge" data-workspace-path="/forge/workspace?view=projects" data-workspace-session-generation="session-a"><div data-extension-workspace-direct-content>Global Workspace</div><aside class="extension-workspace-panels"><div id="terminal-owner">Live terminal</div></aside></main><footer></footer>';
  const owner = document.getElementById('terminal-owner');
  const fetcher = vi.fn(async () => new Response(`<html><head><title>Repository Workspace</title></head><body><main data-workspace-direct data-workspace-session-generation="session-a"><div data-extension-workspace-direct-content><div class="repo-header">team/repo</div><div data-extension-page data-extension-repository-id="7"></div></div></main></body></html>`, {headers: {'X-Extension-Session-Generation': 'session-a'}}));
  initExtensionWorkspace(fetcher);
  expect(document.querySelector('iframe')).toBeNull();
  expect(document.getElementById('navbar').hidden).toBe(false);
  const link = document.createElement('a');
  link.href = '/forge/team/repo/workspace?view=all#details';
  document.body.append(link);
  link.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/forge/team/repo/workspace?view=all#details');
  expect(document.title).toBe('Repository Workspace');
  expect(document.querySelector('.repo-header').textContent).toBe('team/repo');
  expect(document.querySelector('[data-extension-workspace]').dataset.extensionRepositoryId).toBe('7');
  expect(document.getElementById('terminal-owner')).toBe(owner);
  expect(owner.isConnected).toBe(true);
  expect(document.querySelector('iframe')).toBeNull();
  window.dispatchEvent(new Event('pagehide'));
});
