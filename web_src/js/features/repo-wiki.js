import {initMarkupContent} from '../markup/content.js';
import {validateTextareaNonEmpty, initComboMarkdownEditor} from './comp/ComboMarkdownEditor.js';
import {fomanticMobileScreen} from '../modules/fomantic.js';
import {POST} from '../modules/fetch.js';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

let wikiTimer = null;
let wikiGeneration = -1;
let wikiEditor = null;
let wikiForm = null;
let wikiSubmitHandler = null;
let tocHandler = null;

function stopWikiPolling() {
  if (wikiTimer !== null) clearTimeout(wikiTimer);
  wikiTimer = null;
}

async function initRepoWikiFormEditor(generation) {
  const editArea = document.querySelector('.repository.wiki .combo-markdown-editor textarea');
  if (!editArea) return;
  const form = document.querySelector('.repository.wiki.new .ui.form');
  if (!form) return;
  const editorContainer = form.querySelector('.combo-markdown-editor');
  if (!editorContainer) return;
  wikiForm = form;
  let renderRequesting = false;
  let lastContent;
  const renderEasyMDEPreview = async function () {
    if (generation !== currentPageGeneration()) return;
    const previewFull = editorContainer.querySelector('.EasyMDEContainer .editor-preview-active');
    const previewSide = editorContainer.querySelector('.EasyMDEContainer .editor-preview-active-side');
    const previewTarget = previewSide || previewFull;
    const newContent = editArea.value;
    if (wikiEditor && previewTarget && lastContent !== newContent) {
      if (renderRequesting) {
        wikiTimer = setTimeout(renderEasyMDEPreview, 1000);
        return;
      }
      renderRequesting = true;
      const formData = new FormData();
      formData.append('mode', wikiEditor.previewMode);
      formData.append('context', wikiEditor.previewContext);
      formData.append('text', newContent);
      formData.append('wiki', wikiEditor.previewWiki);
      try {
        const response = await POST(wikiEditor.previewUrl, {data: formData});
        const data = await response.text();
        if (generation !== currentPageGeneration()) return;
        lastContent = newContent;
        previewTarget.innerHTML = `<div class="markup ui segment">${data}</div>`;
        initMarkupContent();
      } catch (error) {
        if (generation === currentPageGeneration()) console.error('Error rendering preview:', error);
      } finally {
        renderRequesting = false;
        if (generation === currentPageGeneration()) wikiTimer = setTimeout(renderEasyMDEPreview, 1000);
      }
    } else {
      wikiTimer = setTimeout(renderEasyMDEPreview, 1000);
    }
  };
  wikiTimer = setTimeout(renderEasyMDEPreview, 0);
  const editor = await initComboMarkdownEditor(editorContainer, {
    useScene: 'wiki',
    editorHeights: {minHeight: '300px', height: 'calc(100vh - 600px)'},
    previewMode: 'gfm',
    previewWiki: true,
    easyMDEOptions: {
      previewRender: (_content, previewTarget) => previewTarget.innerHTML,
      toolbar: ['bold', 'italic', 'strikethrough', '|',
        'heading-1', 'heading-2', 'heading-3', 'heading-bigger', 'heading-smaller', '|',
        'gitea-code-inline', 'code', 'quote', '|', 'gitea-checkbox-empty', 'gitea-checkbox-checked', '|',
        'unordered-list', 'ordered-list', '|',
        'link', 'image', 'table', 'horizontal-rule', '|',
        'preview', 'fullscreen', 'side-by-side', '|', 'gitea-switch-to-textarea',
      ],
    },
  });
  if (generation !== currentPageGeneration()) {
    // Retired while EasyMDE loaded: tear the fresh editor back down.
    try { editor.easyMDE?.toTextArea?.() } catch { /* editor never attached */ }
    return;
  }
  wikiEditor = editor;
  wikiSubmitHandler = (e) => {
    if (!validateTextareaNonEmpty(editArea)) {
      e.preventDefault();
      e.stopPropagation();
    }
  };
  form.addEventListener('submit', wikiSubmitHandler);
}

function collapseWikiTocForMobile(collapse) {
  if (collapse) document.querySelector('.wiki-content-toc details')?.removeAttribute('open');
}

function onTocChange(e) {
  collapseWikiTocForMobile(e.matches);
}

export function initRepoWikiForm() {
  if (!document.querySelector('.page-content.repository.wiki')) return;
  if (document.querySelector('.page-content.repository.wiki')?.closest(contributionRootSelector)) return;
  if (tocHandler) return;
  retireRepoWikiForm();
  wikiGeneration = currentPageGeneration();
  tocHandler = onTocChange;
  fomanticMobileScreen.addEventListener('change', tocHandler);
  collapseWikiTocForMobile(fomanticMobileScreen.matches);
  initRepoWikiFormEditor(wikiGeneration);
}

export function retireRepoWikiForm() {
  stopWikiPolling();
  if (tocHandler) fomanticMobileScreen.removeEventListener('change', tocHandler);
  tocHandler = null;
  if (wikiForm && wikiSubmitHandler) wikiForm.removeEventListener('submit', wikiSubmitHandler);
  wikiForm = null;
  wikiSubmitHandler = null;
  if (wikiEditor) {
    try { wikiEditor.easyMDE?.toTextArea?.() } catch { /* already detached */ }
    if (wikiEditor.container) delete wikiEditor.container._giteaComboMarkdownEditor;
  }
  wikiEditor = null;
  wikiGeneration = -1;
}
