import $ from 'jquery';
import {vi} from 'vitest';
import {initRepoSettingsCollaboration, retireRepoSettingsCollaboration} from './repo-settings.js';

vi.mock('./codemirror.ts', () => ({createCodemirror: vi.fn()}));
vi.mock('../modules/fetch.js', () => ({POST: vi.fn()}));

afterEach(() => {
  retireRepoSettingsCollaboration();
  document.body.innerHTML = '';
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
