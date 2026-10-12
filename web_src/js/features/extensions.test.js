import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {registerPageFeature, retirePageFeatures} from '../native-lifecycle.js';
import {classifyWorkspaceNavigation, workspacePath, workspaceDirectPath, initExtensionPages, initExtensionWorkspace} from './extensions.js';

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
    kind: 'native',
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

test('keeps auth and external destinations for top-level navigation', () => {
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
  expect(classifyWorkspaceNavigation('/repo?monkey=usual', base, origin).kind).toBe('native');
  expect(classifyWorkspaceNavigation('https://elsewhere.example/', base, origin).kind).toBe('top');
  expect(classifyWorkspaceNavigation('data:text/html,unsafe', base, origin).kind).toBe('reject');
  expect(workspacePath('//elsewhere.example/', base, origin)).toBeNull();
  expect(workspacePath('/team%2frepo', base, origin)).toBeNull();
  expect(workspacePath('/team%5crepo', base, origin)).toBeNull();
  expect(workspacePath('/elsewhere', `${origin}/forge/team/repo`, origin, '/forge')).toBeNull();
  expect(workspacePath('/forge/team', `${origin}/forge/team/repo`, origin, '/forge')).toBe('/forge/team');
});

// Mount validation, scoped transport and the mount/update/suspend/resume/retire
// lifecycle are owned by extension-mount.js and covered there; these tests
// cover host mounting, session checks and navigation behavior.
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

// NFX-A02: navigation fixtures mirror the real wire format. Fetched documents
// are full native pages (head title, body data-page-data config, navbar,
// content region); the live shell matches templates/extensions/workspace_page.
function navigationDocument({title, pageData, navbar, content}) {
  return `<html><head><title>${title}</title></head><body data-page-data='${JSON.stringify(pageData)}'><nav id="navbar">${navbar}</nav>${content}<footer></footer></body></html>`;
}

function directDocument({title, pageData, content, generation, navbar = ''}) {
  return navigationDocument({title, pageData, navbar,
    content: `<main data-workspace-direct data-workspace-session-generation="${generation}"><div data-extension-workspace-direct-content class="page-content extensions">${content}</div></main>`});
}

function plainAdminDocument({title, pageData, content, navbar = ''}) {
  return navigationDocument({title, pageData, navbar,
    content: `<div role="main" class="page-content admin"><div class="ui container flex-container"><div class="admin-navbar">Admin</div><div class="flex-container-main">${content}</div></div></div>`});
}

function shellMarkup({path, session = 'session-a', slot = 'Global Workspace', panels = '<div id="notes-owner">notes</div>', navbar = ''}) {
  return `<nav id="navbar">${navbar}</nav><main data-extension-workspace data-workspace-direct data-app-sub-url="" data-workspace-path="${path}" data-workspace-session-generation="${session}"><div data-extension-workspace-direct-content class="page-content extensions">${slot}</div><aside class="extension-workspace-panels">${panels}</aside></main><footer></footer>`;
}

function clickWorkspaceLink(href) {
  const link = document.createElement('a');
  link.href = href;
  document.body.append(link);
  link.click();
  link.remove();
}

function navigationResponse(html, generation = 'session-a') {
  return new Response(html, {headers: {'X-Extension-Session-Generation': generation}});
}

test('transplant keeps one shell, panels and address across contribution -> native', async () => {
  const savedConfig = window.config;
  window.history.replaceState({nativeState: 'retained'}, '', '/workspace');
  document.body.innerHTML = shellMarkup({path: '/workspace', navbar: '<a class="item" href="/admin/users">Users</a><a class="item active" aria-current="page" href="/workspace">Workspace</a>'});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const owner = document.getElementById('notes-owner');
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    plainAdminDocument({title: 'Users - Forgejo', pageData: adminPageData, content: '<form id="user-list-search-form"></form>',
      navbar: '<a class="item" href="/admin/users">Users</a><a class="item" href="/workspace">Workspace</a>'}) :
    directDocument({title: 'Workspace', pageData: workspacePageData, content: 'Global Workspace', generation: 'session-a',
      navbar: '<a class="item" href="/admin/users">Users</a><a class="item active" aria-current="page" href="/workspace">Workspace</a>'})));
  const pushState = vi.spyOn(window.history, 'pushState');
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users?sort=oldest#list');
    await vi.waitFor(() => expect(window.location.pathname + window.location.search + window.location.hash).toBe('/admin/users?sort=oldest#list'));
    expect(document.title).toBe('Users - Forgejo');
    expect(document.querySelector('#user-list-search-form')).not.toBeNull();
    expect(window.config.pageData).toEqual(adminPageData);
    expect(document.getElementById('notes-owner')).toBe(owner);
    expect(owner.isConnected).toBe(true);
    expect(pushState).toHaveBeenCalledOnce();
    expect(document.querySelector('#navbar a[href="/workspace"]').classList.contains('active')).toBe(false);
    expect(document.querySelector('#navbar a[href="/workspace"]').hasAttribute('aria-current')).toBe(false);
    window.history.replaceState({nativeState: 'retained'}, '', '/workspace');
    window.dispatchEvent(new PopStateEvent('popstate'));
    await vi.waitFor(() => expect(document.querySelector('[data-extension-workspace-direct-content]').textContent).toBe('Global Workspace'));
    expect(window.config.pageData).toEqual(workspacePageData);
    expect(window.history.state.nativeState).toBe('retained');
    expect(document.getElementById('notes-owner')).toBe(owner);
  } finally {
    pushState.mockRestore();
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('transplant delivers ordered presentation updates to retained panels', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = shellMarkup({path: '/workspace',
    panels: '<div data-extension-panel data-app-sub-url="" data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/native/assets/example/panel.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/panels/example/notes/api/" data-extension-session-generation="session-a"></div>'});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const update = vi.fn();
  const dispose = vi.fn();
  let mountedResolve;
  const mountedPromise = new Promise((resolve) => { mountedResolve = resolve });
  const importer = vi.fn(async () => ({mount: () => {
    mountedResolve();
    return {dispose, update};
  }}));
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const fetcher = vi.fn(async () => navigationResponse(plainAdminDocument({title: 'Users', pageData: adminPageData, content: '<form></form>'})));
  try {
    initExtensionPages(importer);
    await mountedPromise;
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(importer).toHaveBeenCalledOnce();
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users?sort=name');
    await vi.waitFor(() => expect(update).toHaveBeenCalledOnce());
    expect(update).toHaveBeenCalledWith({route: '/admin/users?sort=name', params: {sort: 'name'}, mode: 'panel', visible: true});
    expect(dispose).not.toHaveBeenCalled();
    expect(importer).toHaveBeenCalledOnce();
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('transplant remounts page roots and disposes the detached owner', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = shellMarkup({path: '/workspace',
    slot: '<div data-extension-page data-app-sub-url="" data-extension-id="example" data-extension-page-id="home" data-extension-entry="/-/native/assets/example/page.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/pages/example/home/api/" data-extension-session-generation="session-a"></div>'});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const firstDispose = vi.fn();
  const secondMount = vi.fn();
  secondMount.mockReturnValue({});
  let mountedResolve;
  const mountedPromise = new Promise((resolve) => { mountedResolve = resolve });
  const firstMount = () => {
    mountedResolve();
    return firstDispose;
  };
  const importer = vi.fn(async () => ({mount: firstMount}));
  const fetcher = vi.fn(async () => navigationResponse(directDocument({title: 'Workspace', pageData: {extension: {id: 'example'}},
    content: '<div data-extension-page data-app-sub-url="" data-extension-id="example" data-extension-page-id="home" data-extension-entry="/-/native/assets/example/page.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/pages/example/home/api/" data-extension-session-generation="session-a"></div>',
    generation: 'session-a'})));
  try {
    initExtensionPages(importer);
    await mountedPromise;
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(importer).toHaveBeenCalledOnce();
    importer.mockImplementationOnce(async () => ({mount: secondMount}));
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/workspace?view=all');
    await vi.waitFor(() => expect(firstDispose).toHaveBeenCalledOnce());
    await vi.waitFor(() => expect(secondMount).toHaveBeenCalledOnce());
    expect(importer).toHaveBeenCalledTimes(2);
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('leaves modified links, downloads and excluded destinations to native handling', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = shellMarkup({path: '/workspace'});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const fetcher = vi.fn(async () => navigationResponse(directDocument({title: 'Workspace', pageData: {}, content: '', generation: 'session-a'})));
  try {
    initExtensionWorkspace(fetcher);
    const modified = document.createElement('a');
    modified.href = '/admin/users';
    document.body.append(modified);
    modified.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, ctrlKey: true}));
    expect(fetcher).not.toHaveBeenCalled();
    modified.remove();
    const download = document.createElement('a');
    download.href = '/admin/users';
    download.download = 'users';
    document.body.append(download);
    download.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true}));
    expect(fetcher).not.toHaveBeenCalled();
    download.remove();
    const blocked = document.createElement('a');
    blocked.href = '/-/native/session';
    document.body.append(blocked);
    const rejected = new MouseEvent('click', {bubbles: true, cancelable: true});
    blocked.dispatchEvent(rejected);
    expect(rejected.defaultPrevented).toBe(true);
    expect(fetcher).not.toHaveBeenCalled();
    blocked.remove();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(document.querySelector('[data-extension-workspace-direct-content]').textContent).toBe('Global Workspace');
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('takes a full document load for unknown layouts', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = shellMarkup({path: '/workspace'});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const fetcher = vi.fn(async () => navigationResponse(directDocument({title: 'Workspace', pageData: {}, content: '', generation: 'session-a'})));
  const assign = vi.spyOn(window.location, 'assign').mockImplementation(() => {});
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/team/repo/issues');
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(fetcher).not.toHaveBeenCalled();
    expect(assign).toHaveBeenCalledWith(expect.stringContaining('/team/repo/issues'));
    expect(document.querySelector('[data-extension-workspace-direct-content]').textContent).toBe('Global Workspace');
  } finally {
    assign.mockRestore();
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('workspace uses its declared subpath for session checks while retaining the native deep link', async () => {
  document.body.innerHTML = '<nav id="navbar"></nav><main data-extension-workspace data-workspace-direct data-app-sub-url="/forge" data-workspace-path="/forge/workspace" data-workspace-session-generation="session-a"><div data-extension-workspace-direct-content>Global Workspace</div></main><footer></footer>';
  window.history.replaceState(null, '', '/forge/workspace#comment-1');
  const fetcher = vi.fn().mockResolvedValue({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  initExtensionWorkspace(fetcher);
  window.dispatchEvent(new Event('focus'));
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledWith(`${window.location.origin}/forge/-/native/session`, {
    credentials: 'same-origin', cache: 'no-store', redirect: 'manual',
  }));
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/forge/workspace#comment-1');
  window.dispatchEvent(new Event('pagehide'));
});

test('hides a restored workspace until the live native generation matches and retires owners on account change', async () => {
  document.body.innerHTML = '<main data-extension-workspace data-workspace-direct data-app-sub-url="" data-workspace-path="/workspace" data-workspace-session-generation="session-a"><div data-extension-workspace-direct-content>Global Workspace</div><aside class="extension-workspace-panels"><div data-extension-panel data-app-sub-url="" data-extension-id="example" data-extension-panel-id="notes" data-extension-entry="/-/native/assets/example/panel.js" data-extension-asset-base="/-/native/assets/example/" data-extension-api-base="/-/native/panels/example/notes/api/" data-extension-session-generation="session-a"></div></aside></main>';
  window.history.replaceState(null, '', '/workspace');
  const workspace = document.querySelector('[data-extension-workspace]');
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
  expect(workspace.querySelector('[data-extension-panel]')).toBeNull();
  expect(dispose).toHaveBeenCalledOnce();
  window.dispatchEvent(new Event('pagehide'));
});

test('ignores a session check completed after the workspace was cached', async () => {
  document.body.innerHTML = '<main data-extension-workspace data-workspace-direct data-app-sub-url="" data-workspace-path="/workspace" data-workspace-session-generation="session-a"><div data-extension-workspace-direct-content>Global Workspace</div></main>';
  window.history.replaceState(null, '', '/workspace');
  const workspace = document.querySelector('[data-extension-workspace]');
  const slot = workspace.querySelector('[data-extension-workspace-direct-content]');
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
  expect(workspace.querySelector('[data-extension-workspace-direct-content]')).toBe(slot);
  resolveCurrent({status: 204, headers: new Headers({'X-Extension-Session-Generation': 'session-a'})});
  await vi.waitFor(() => expect(workspace.style.display).toBe(''));
  window.dispatchEvent(new Event('pagehide'));
});

// NFX-A01/H-0, proved by NFX-A02: both entry orders run the real transplant
// chain across the direct contribution shape and the plain native shape,
// keeping one retained owner while the live page config follows each page.
test('contribution -> native -> contribution refreshes live page config with one retained owner', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = shellMarkup({path: '/workspace'});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const owner = document.getElementById('notes-owner');
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    plainAdminDocument({title: 'Users - Forgejo', pageData: adminPageData, content: '<form id="user-list-search-form"></form>'}) :
    directDocument({title: 'Workspace', pageData: workspacePageData, content: 'Global Workspace', generation: 'session-a'})));
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/admin/users'));
    expect(document.title).toBe('Users - Forgejo');
    expect(document.querySelector('#user-list-search-form')).not.toBeNull();
    expect(window.config.pageData).toEqual(adminPageData);
    expect(document.getElementById('notes-owner')).toBe(owner);
    clickWorkspaceLink('/workspace');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace'));
    expect(document.title).toBe('Workspace');
    expect(window.config.pageData).toEqual(workspacePageData);
    expect(document.getElementById('notes-owner')).toBe(owner);
    expect(owner.isConnected).toBe(true);
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('native -> contribution -> native refreshes live page config with one retained owner', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/admin/users');
  document.body.innerHTML = shellMarkup({path: '/admin/users', slot: '<form id="user-list-search-form"></form>'});
  window.config = {appSubUrl: '', pageData: {adminUserListSearchForm: {SortType: 'name'}}};
  const owner = document.getElementById('notes-owner');
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    plainAdminDocument({title: 'Users - Forgejo', pageData: adminPageData, content: '<form id="user-list-search-form"></form>'}) :
    directDocument({title: 'Workspace', pageData: workspacePageData, content: 'Global Workspace', generation: 'session-a'})));
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/workspace?view=all');
    await vi.waitFor(() => expect(window.location.pathname + window.location.search).toBe('/workspace?view=all'));
    expect(document.title).toBe('Workspace');
    expect(window.config.pageData).toEqual(workspacePageData);
    expect(document.getElementById('notes-owner')).toBe(owner);
    clickWorkspaceLink('/admin/users');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/admin/users'));
    expect(document.title).toBe('Users - Forgejo');
    expect(document.querySelector('#user-list-search-form')).not.toBeNull();
    expect(window.config.pageData).toEqual(adminPageData);
    expect(document.getElementById('notes-owner')).toBe(owner);
    expect(owner.isConnected).toBe(true);
  } finally {
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});

test('brackets transplants with retire, config refresh and init in order', async () => {
  const savedConfig = window.config;
  window.history.replaceState(null, '', '/workspace');
  document.body.innerHTML = shellMarkup({path: '/workspace'});
  window.config = {appSubUrl: '', pageData: {extension: {id: 'example'}}};
  const order = [];
  let observedConfig = null;
  registerPageFeature('a02-bracketing-probe', {
    init: () => {
      order.push('init');
      observedConfig = window.config.pageData;
    },
    retire: () => {
      order.push('retire');
    },
  });
  const adminPageData = {adminUserListSearchForm: {SortType: 'name'}};
  const workspacePageData = {extension: {id: 'example'}};
  const fetcher = vi.fn(async (url) => navigationResponse(url.includes('/admin/users') ?
    plainAdminDocument({title: 'Users', pageData: adminPageData, content: '<form></form>'}) :
    directDocument({title: 'Workspace', pageData: workspacePageData, content: 'Global Workspace', generation: 'session-a'})));
  try {
    initExtensionWorkspace(fetcher);
    clickWorkspaceLink('/admin/users');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/admin/users'));
    expect(order).toEqual(['init']);
    expect(observedConfig).toEqual(adminPageData);
    clickWorkspaceLink('/workspace');
    await vi.waitFor(() => expect(window.location.pathname).toBe('/workspace'));
    expect(order).toEqual(['init', 'retire', 'init']);
  } finally {
    retirePageFeatures();
    window.dispatchEvent(new Event('pagehide'));
    window.config = savedConfig;
  }
});
