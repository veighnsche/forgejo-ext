import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {initRepoRelease, retireRepoRelease, initRepoReleaseNew, retireRepoReleaseNew} from './repo-release.js';

function installRelease() {
  document.body.innerHTML = `<div class="page-content"><button class="remove-rel-attach" data-uuid="u1" data-id="a1">remove</button>
    <input name="attachment-del-u1" value="false"><div id="attachment-a1">file</div>
    <div data-extension-panel><button class="remove-rel-attach" data-uuid="owned" data-id="owned">owned</button></div>
    <input name="attachment-del-owned" value="false"><div id="attachment-owned">owned</div></div>`;
}

function installReleaseNew() {
  document.body.innerHTML = `<div class="page-content repository new release">
    <div id="tag-name-editor" data-existing-tags='["v1.0"]' data-tag-helper="pick" data-tag-helper-new="new tag" data-tag-helper-existing="exists"></div>
    <input id="tag-name" value=""><div id="tag-helper"></div><div id="tag-target-selector"></div>
    <input id="release-title" value="">
    <button id="add-external-link" type="button">add link</button>
    <div id="attachment-template" class="tw-hidden"><input name="attachment-template-new-name"><input name="attachment-template-new-exturl"><button class="remove-rel-attach" type="button">x</button></div>
  </div>`;
}

afterEach(() => {
  retireRepoRelease();
  retireRepoReleaseNew();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('release attachment removal marks current files and retire detaches', () => {
  installRelease();
  initRepoRelease();
  initRepoRelease();
  document.querySelector('.page-content .remove-rel-attach').click();
  expect(document.querySelector(`input[name='attachment-del-u1']`).value).toBe('true');
  expect(document.getElementById('attachment-a1').classList.contains('tw-hidden')).toBe(true);
  // contribution-owned attachments are never bound
  document.querySelector('[data-extension-panel] .remove-rel-attach').click();
  expect(document.querySelector(`input[name='attachment-del-owned']`).value).toBe('false');
  retireRepoRelease();
  document.querySelector(`input[name='attachment-del-u1']`).value = 'false';
  document.getElementById('attachment-a1').classList.remove('tw-hidden');
  document.querySelector('.page-content .remove-rel-attach').click();
  expect(document.querySelector(`input[name='attachment-del-u1']`).value).toBe('false');
  // re-entry with fresh nodes works
  installRelease();
  initRepoRelease();
  document.querySelector('.page-content .remove-rel-attach').click();
  expect(document.querySelector(`input[name='attachment-del-u1']`).value).toBe('true');
});

test('release-new tag editor follows current input and retire detaches', async () => {
  installReleaseNew();
  await initRepoReleaseNew();
  await initRepoReleaseNew();
  const tag = document.getElementById('tag-name');
  tag.value = 'v1.0';
  tag.dispatchEvent(new Event('keyup'));
  expect(document.getElementById('tag-target-selector').classList.contains('tw-hidden')).toBe(true);
  expect(document.getElementById('tag-helper').textContent).toBe('exists');
  tag.value = 'v2.0';
  tag.dispatchEvent(new Event('keyup'));
  expect(document.getElementById('tag-target-selector').classList.contains('tw-hidden')).toBe(false);
  expect(document.getElementById('release-title').value).toBe('v2.0');
  retireRepoReleaseNew();
  tag.value = 'v1.0';
  tag.dispatchEvent(new Event('keyup'));
  expect(document.getElementById('tag-helper').textContent).toBe('new tag');
});

test('release-new external links clone with fresh names and retire detaches', async () => {
  installReleaseNew();
  await initRepoReleaseNew();
  document.getElementById('add-external-link').click();
  document.getElementById('add-external-link').click();
  expect(document.querySelector('input[name="attachment-new-name-1"]')).not.toBeNull();
  expect(document.querySelector('input[name="attachment-new-name-2"]')).not.toBeNull();
  document.querySelector('#attachment-N1 .remove-rel-attach').click();
  expect(document.getElementById('attachment-N1')).toBeNull();
  retireRepoReleaseNew();
  document.getElementById('add-external-link').click();
  expect(document.querySelector('input[name="attachment-new-name-3"]')).toBeNull();
});

test('page seam drives release init and retire', () => {
  installRelease();
  registerPageFeature('test-b06-release', {init: initRepoRelease, retire: retireRepoRelease});
  registerPageFeature('test-b06-release-new', {init: () => initRepoReleaseNew(), retire: retireRepoReleaseNew});
  setPageConfig({});
  initPageFeatures();
  document.querySelector('.page-content .remove-rel-attach').click();
  expect(document.querySelector(`input[name='attachment-del-u1']`).value).toBe('true');
  retirePageFeatures();
  document.querySelector(`input[name='attachment-del-u1']`).value = 'false';
  document.querySelector('.page-content .remove-rel-attach').click();
  expect(document.querySelector(`input[name='attachment-del-u1']`).value).toBe('false');
});
