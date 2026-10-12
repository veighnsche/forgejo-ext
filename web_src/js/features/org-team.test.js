import $ from 'jquery';
import {vi} from 'vitest';
import {initOrgTeamSearchRepoBox, retireOrgTeamSearchRepoBox} from './org-team.js';

const searchCalls = [];

beforeEach(() => {
  searchCalls.length = 0;
  $.fn.search = vi.fn(function (...args) {
    searchCalls.push(args);
    return this;
  });
});

afterEach(() => {
  retireOrgTeamSearchRepoBox();
  delete $.fn.search;
  document.body.innerHTML = '';
});

test('init configures the search widget once with the owner uid', () => {
  document.body.innerHTML = `<div id="search-repo-box" data-uid="42"></div>`;
  initOrgTeamSearchRepoBox();
  initOrgTeamSearchRepoBox();
  expect(searchCalls).toHaveLength(1);
  const settings = searchCalls[0][0];
  expect(settings.apiSettings.url).toBe('/repo/search?q={query}&uid=42');
  expect(settings.searchFields).toEqual(['full_name']);
  const response = settings.apiSettings.onResponse({data: [{repository: {full_name: 'org/repo'}}]});
  expect(response).toEqual({results: [{title: 'repo', description: 'org/repo'}]});
});

test('retire destroys the widget and re-init configures it again', () => {
  document.body.innerHTML = `<div id="search-repo-box" data-uid="42"></div>`;
  initOrgTeamSearchRepoBox();
  retireOrgTeamSearchRepoBox();
  expect(searchCalls).toEqual([[expect.any(Object)], ['destroy']]);
  initOrgTeamSearchRepoBox();
  expect(searchCalls).toHaveLength(3);
});

test('init without the box and retire without init are no-ops', () => {
  document.body.innerHTML = '';
  expect(() => initOrgTeamSearchRepoBox()).not.toThrow();
  expect(() => retireOrgTeamSearchRepoBox()).not.toThrow();
  expect(searchCalls).toHaveLength(0);
});
