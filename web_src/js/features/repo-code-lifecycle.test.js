import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {initRepoCodeView, retireRepoCodeView} from './repo-code.ts';

function installFold() {
  document.body.innerHTML = `<div class="page-content"><div class="file-content" data-folded="false"><button class="fold-file" type="button"></button></div></div>`;
  return document.querySelector('.fold-file');
}

afterEach(() => {
  retireRepoCodeView();
  retirePageFeatures();
  document.body.innerHTML = '';
  window.location.hash = '';
});

test('fold handling binds once and retire removes it', () => {
  const button = installFold();
  const box = document.querySelector('.file-content');
  initRepoCodeView();
  initRepoCodeView();
  button.click();
  expect(box.getAttribute('data-folded')).toBe('true');
  button.click();
  expect(box.getAttribute('data-folded')).toBe('false');
  retireRepoCodeView();
  button.click();
  expect(box.getAttribute('data-folded')).toBe('false');
  initRepoCodeView();
  button.click();
  expect(box.getAttribute('data-folded')).toBe('true');
});

test('retired code view leaves contribution-owned folds untouched', () => {
  document.body.innerHTML = `<div class="page-content"><div data-extension-panel><div class="file-content" data-folded="false"><button class="fold-file" id="owned-fold" type="button"></button></div></div>
    <div class="file-content" data-folded="false"><button class="fold-file" id="native-fold" type="button"></button></div></div>`;
  const ownedBox = document.querySelector('#owned-fold').closest('.file-content');
  const nativeBox = document.querySelector('#native-fold').closest('.file-content');
  initRepoCodeView();
  document.querySelector('#owned-fold').click();
  expect(ownedBox.getAttribute('data-folded')).toBe('false');
  document.querySelector('#native-fold').click();
  expect(nativeBox.getAttribute('data-folded')).toBe('true');
  retireRepoCodeView();
});

test('page seam drives code view init and retire', () => {
  const button = installFold();
  const box = document.querySelector('.file-content');
  registerPageFeature('test-b04-code', {init: initRepoCodeView, retire: retireRepoCodeView});
  setPageConfig({});
  initPageFeatures();
  button.click();
  expect(box.getAttribute('data-folded')).toBe('true');
  retirePageFeatures();
  button.click();
  expect(box.getAttribute('data-folded')).toBe('true');
});
