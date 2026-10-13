import {getComboMarkdownEditor, initComboMarkdownEditor} from './comp/ComboMarkdownEditor.js';
import {contributionRootSelector} from '../native-lifecycle.js';

let milestoneEditor = null;

export function retireRepoMilestoneEditor() {
  milestoneEditor?.destroy();
  milestoneEditor = null;
}

export async function initRepoMilestoneEditor() {
  retireRepoMilestoneEditor();
  const editor = document.querySelector('.page-content.repository.milestone .combo-markdown-editor');
  if (!editor || editor.closest(contributionRootSelector)) {
    return;
  }
  const initialized = initComboMarkdownEditor(editor);
  milestoneEditor = getComboMarkdownEditor(editor);
  const instance = await initialized;
  if (milestoneEditor !== instance || !editor.isConnected) instance.destroy();
}
