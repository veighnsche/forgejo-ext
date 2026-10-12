import {vi} from 'vitest';
import {initAdminUserListSearchForm, retireAdminUserListSearchForm} from './users.js';

const savedPageData = window.config.pageData;

function installForm() {
  document.body.innerHTML = `<form id="user-list-search-form" action="/admin/users" method="get">
    <button type="button" name="sort" value="name_asc">Name</button>
    <button type="button" name="sort" value="created_desc">Created</button>
    <input type="radio" name="status_filter[active]" value="true">
    <input type="radio" name="status_filter[active]" value="false">
    <a class="item j-reset-status-filter">Reset</a>
  </form>`;
  return document.querySelector('#user-list-search-form');
}

// Models the host seam's interception contract: a canceling submit listener
// records the navigation the transplant would perform. The control must flow
// through the submit event so the host can retain the shell.
function interceptSubmits(form) {
  const navigations = [];
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const url = new URL(form.action);
    for (const [key, value] of new FormData(form)) {
      url.searchParams.append(key, value.toString());
    }
    navigations.push(url.pathname + url.search);
  });
  return navigations;
}

beforeEach(() => {
  window.config.pageData = {adminUserListSearchForm: {SortType: 'name_asc', StatusFilterMap: {active: 'true'}}};
});

afterEach(() => {
  window.config.pageData = savedPageData;
  document.body.innerHTML = '';
});

test('init applies the page configuration to the search form', () => {
  const form = installForm();
  initAdminUserListSearchForm();
  expect(form.querySelector('button[value="name_asc"]').classList.contains('active')).toBe(true);
  expect(form.querySelector('button[value="created_desc"]').classList.contains('active')).toBe(false);
  expect(form.querySelector('input[value="true"]').checked).toBe(true);
  expect(form.querySelector('input[value="false"]').checked).toBe(false);
  retireAdminUserListSearchForm();
});

test('radio clicks submit through requestSubmit with an interceptable submit event', () => {
  const form = installForm();
  const navigations = interceptSubmits(form);
  const request = vi.spyOn(form, 'requestSubmit');
  initAdminUserListSearchForm();
  initAdminUserListSearchForm();
  form.querySelector('input[value="false"]').click();
  expect(request).toHaveBeenCalledTimes(1);
  expect(navigations).toHaveLength(1);
  expect(navigations[0]).toContain('status_filter%5Bactive%5D=false');
  retireAdminUserListSearchForm();
});

test('retire removes listeners and the form re-initializes cleanly', () => {
  const form = installForm();
  const navigations = interceptSubmits(form);
  initAdminUserListSearchForm();
  retireAdminUserListSearchForm();
  form.querySelector('input[value="true"]').click();
  form.querySelector('.j-reset-status-filter').click();
  expect(navigations).toHaveLength(0);

  window.config.pageData = {adminUserListSearchForm: {SortType: 'created_desc'}};
  initAdminUserListSearchForm();
  expect(form.querySelector('button[value="created_desc"]').classList.contains('active')).toBe(true);
  form.querySelector('input[value="false"]').click();
  expect(navigations).toHaveLength(1);
  retireAdminUserListSearchForm();
});

test('reset clears status filters and submits through the submit event', () => {
  const form = installForm();
  const navigations = interceptSubmits(form);
  initAdminUserListSearchForm();
  form.querySelector('.j-reset-status-filter').click();
  expect(form.querySelector('input[value="true"]').checked).toBe(false);
  expect(navigations).toHaveLength(1);
  expect(navigations[0]).not.toContain('status_filter');
  retireAdminUserListSearchForm();
});

test('init without config or form and retire without init are no-ops', () => {
  window.config.pageData = {};
  installForm();
  expect(() => initAdminUserListSearchForm()).not.toThrow();
  document.body.innerHTML = '';
  window.config.pageData = {adminUserListSearchForm: {SortType: 'name_asc'}};
  expect(() => initAdminUserListSearchForm()).not.toThrow();
  expect(() => retireAdminUserListSearchForm()).not.toThrow();
});
