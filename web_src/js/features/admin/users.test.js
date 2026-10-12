import {initAdminUserListSearchForm, retireAdminUserListSearchForm} from './users.js';

const savedPageData = window.config.pageData;

function installForm() {
  document.body.innerHTML = `<form id="user-list-search-form" action="/admin/users">
    <button type="button" name="sort" value="name_asc">Name</button>
    <button type="button" name="sort" value="created_desc">Created</button>
    <input type="radio" name="status_filter[active]" value="true">
    <input type="radio" name="status_filter[active]" value="false">
    <a class="item j-reset-status-filter">Reset</a>
  </form>`;
  const form = document.querySelector('#user-list-search-form');
  form.submit = () => form.dispatchEvent(new Event('form-submitted'));
  return form;
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

test('init attaches each listener once and radio clicks submit', () => {
  const form = installForm();
  let submits = 0;
  form.addEventListener('form-submitted', () => submits++);
  initAdminUserListSearchForm();
  initAdminUserListSearchForm();
  form.querySelector('input[value="true"]').click();
  expect(submits).toBe(1);
  retireAdminUserListSearchForm();
});

test('retire removes listeners and the form re-initializes cleanly', () => {
  const form = installForm();
  let submits = 0;
  form.addEventListener('form-submitted', () => submits++);
  initAdminUserListSearchForm();
  retireAdminUserListSearchForm();
  form.querySelector('input[value="true"]').click();
  form.querySelector('.j-reset-status-filter').click();
  expect(submits).toBe(0);

  window.config.pageData = {adminUserListSearchForm: {SortType: 'created_desc'}};
  initAdminUserListSearchForm();
  expect(form.querySelector('button[value="created_desc"]').classList.contains('active')).toBe(true);
  form.querySelector('input[value="false"]').click();
  expect(submits).toBe(1);
  retireAdminUserListSearchForm();
});

test('reset clears status filters and submits', () => {
  const form = installForm();
  let submits = 0;
  form.addEventListener('form-submitted', () => submits++);
  initAdminUserListSearchForm();
  form.querySelector('.j-reset-status-filter').click();
  expect(form.querySelector('input[value="true"]').checked).toBe(false);
  expect(submits).toBe(1);
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
