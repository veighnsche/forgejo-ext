import {setNativeNavigator} from '../native-lifecycle.js';
import {initTableSort, retireTableSort} from './tablesort.js';

afterEach(() => {
  retireTableSort();
  setNativeNavigator(null);
  document.body.innerHTML = '';
  window.history.replaceState(null, '', '/');
});

function installTable() {
  document.body.innerHTML = `<table><thead><tr>
    <th data-sortt-asc="oldest" data-sortt-desc="newest">ID</th>
    <th data-sortt-asc="alphabetically" data-sortt-desc="reversealphabetically" data-sortt-default="true">Name</th>
  </tr></thead></table>`;
}

test('sort clicks navigate once through the plugged host navigator', () => {
  installTable();
  const seen = [];
  setNativeNavigator((url) => seen.push(url));
  initTableSort();
  initTableSort();
  window.history.replaceState(null, '', '/admin/orgs?sort=oldest');
  document.querySelector('th[data-sortt-asc="oldest"]').click();
  expect(seen).toHaveLength(1);
  expect(seen[0]).toContain('/admin/orgs?sort=newest');
  retireTableSort();
});

test('retire removes sort listeners', () => {
  installTable();
  const seen = [];
  setNativeNavigator((url) => seen.push(url));
  initTableSort();
  retireTableSort();
  document.querySelector('th[data-sortt-asc="oldest"]').click();
  expect(seen).toHaveLength(0);
});

test.each(['data-extension-panels', 'data-extension-page', 'data-extension-panel'])('native sort lifecycle leaves retained %s controls untouched', (attribute) => {
  document.body.innerHTML = `<div class="page-content">
    <table><thead><tr><th id="native-sort" data-sortt-asc="oldest" data-sortt-desc="newest">ID</th></tr></thead></table>
    <div ${attribute}>
      <table><thead><tr><th id="retained-sort" data-sortt-asc="oldest" data-sortt-desc="newest">Contribution ID</th></tr></thead></table>
    </div>
  </div>`;
  const nativeHeader = document.querySelector('#native-sort');
  const retainedHeader = document.querySelector('#retained-sort');
  const ownerClick = vi.fn();
  retainedHeader.addEventListener('click', ownerClick);
  const seen = [];
  setNativeNavigator((url) => seen.push(url));
  window.history.replaceState(null, '', '/admin/orgs?sort=oldest');

  initTableSort();
  initTableSort();
  retainedHeader.click();
  expect(ownerClick).toHaveBeenCalledTimes(1);
  expect(seen).toHaveLength(0);
  nativeHeader.click();
  expect(seen).toHaveLength(1);
  expect(seen[0]).toContain('/admin/orgs?sort=newest');

  retireTableSort();
  retainedHeader.click();
  expect(ownerClick).toHaveBeenCalledTimes(2);
  nativeHeader.click();
  expect(seen).toHaveLength(1);

  initTableSort();
  retainedHeader.click();
  expect(ownerClick).toHaveBeenCalledTimes(3);
  expect(seen).toHaveLength(1);
  nativeHeader.click();
  expect(seen).toHaveLength(2);
});

test('default sort applies when no sort param is present', () => {
  installTable();
  const seen = [];
  setNativeNavigator((url) => seen.push(url));
  initTableSort();
  window.history.replaceState(null, '', '/admin/orgs');
  document.querySelector('th[data-sortt-asc="alphabetically"]').click();
  expect(seen).toHaveLength(1);
  expect(seen[0]).toContain('sort=reversealphabetically');
  retireTableSort();
});

test('sort falls back to a native load without a host navigator', () => {
  installTable();
  initTableSort();
  window.history.replaceState(null, '', '/admin/orgs?sort=oldest');
  document.querySelector('th[data-sortt-asc="oldest"]').click();
  expect(window.location.pathname + window.location.search).toBe('/admin/orgs?sort=newest');
  retireTableSort();
});
