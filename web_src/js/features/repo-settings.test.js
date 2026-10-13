import $ from 'jquery';
import {vi} from 'vitest';
import {initRepoSettingsCollaboration, retireRepoSettingsCollaboration, initRepoSettingSearchTeamBox, retireRepoSettingSearchTeamBox} from './repo-settings.js';

vi.mock('./codemirror.ts', () => ({createCodemirror: vi.fn()}));
vi.mock('../modules/fetch.js', () => ({POST: vi.fn()}));

afterEach(() => {
  $.fn.search ??= vi.fn(function () { return this });
  retireRepoSettingsCollaboration();
  retireRepoSettingSearchTeamBox();
  document.body.innerHTML = '';
  delete $.fn.search;
});

test('collaboration access-mode dropdowns configure once and destroy on retire', () => {
  document.body.innerHTML = `<div class="page-content repository">
    <div class="ui dropdown access-mode" data-uid="1" data-url="/collab/1" data-last-value="write"><span class="text">Write</span></div>
    <div class="ui dropdown access-mode" data-uid="2" data-url="/collab/2" data-last-value="read"><span class="text">Read</span></div>
    <div data-extension-page>
      <div class="ui dropdown access-mode" id="retained-mode" data-uid="3" data-url="/collab/3"><span class="text">Retained</span></div>
    </div>
  </div>`;
  const dropdown = vi.spyOn($.fn, 'dropdown');
  try {
    initRepoSettingsCollaboration();
    initRepoSettingsCollaboration();
    const configured = [];
    for (const [i, call] of dropdown.mock.calls.entries()) {
      if (call[0] && typeof call[0] === 'object') configured.push(...dropdown.mock.contexts[i].get());
    }
    expect(configured).toHaveLength(2);
    expect(configured).not.toContain(document.querySelector('#retained-mode'));
    retireRepoSettingsCollaboration();
    const destroyed = [];
    for (const [i, call] of dropdown.mock.calls.entries()) {
      if (call[0] === 'destroy') destroyed.push(...dropdown.mock.contexts[i].get());
    }
    expect(destroyed).toHaveLength(2);
  } finally {
    dropdown.mockRestore();
  }
});

test('team search box configures once with current org and retire destroys', () => {
  document.body.innerHTML = `<div class="page-content"><div id="search-team-box" data-org-name="acme"></div></div>`;
  const search = vi.fn(function () { return this });
  $.fn.search = search;
  try {
    initRepoSettingSearchTeamBox();
    initRepoSettingSearchTeamBox();
    const configs = search.mock.calls.filter((c) => typeof c[0] === 'object');
    expect(configs).toHaveLength(1);
    expect(configs[0][0].apiSettings.url).toContain('/org/acme/teams/-/search');
    retireRepoSettingSearchTeamBox();
    expect(search).toHaveBeenLastCalledWith('destroy');
    // re-entry with fresh org uses current context
    document.body.innerHTML = `<div class="page-content"><div id="search-team-box" data-org-name="globex"></div></div>`;
    search.mockClear();
    initRepoSettingSearchTeamBox();
    expect(search.mock.calls[0][0].apiSettings.url).toContain('/org/globex/');
    retireRepoSettingSearchTeamBox();
  } finally {
    delete $.fn.search;
  }
});

test('contribution-owned team search is never bound', () => {
  document.body.innerHTML = `<div data-extension-panel><div id="search-team-box" data-org-name="owned"></div></div>`;
  const search = vi.fn(function () { return this });
  $.fn.search = search;
  try {
    initRepoSettingSearchTeamBox();
    expect(search).not.toHaveBeenCalled();
  } finally {
    delete $.fn.search;
  }
});
