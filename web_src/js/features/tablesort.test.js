import {setNativeNavigator} from '../native-lifecycle.js';
import {initTableSort, retireTableSort} from './tablesort.js';

afterEach(() => {
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
