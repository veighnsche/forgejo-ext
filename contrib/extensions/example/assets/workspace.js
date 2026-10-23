import {mount as mountPage} from './page.js';
import {mount as mountPanel} from './panel.js';

/**
 * @param {HTMLElement} root
 * @param {import('../../../../sdk/browser/extension-contract.js').MountContext} context
 * @returns {Promise<import('../../../../sdk/browser/extension-contract.js').MountHandle>}
 */
export async function mount(root, context) {
  const disposePage = await mountPage(root, context);
  const panel = mountPanel(root, context);
  return {
    ...panel,
    retire: (reason) => {
      panel.retire?.(reason);
      disposePage();
    },
    dispose: () => {
      panel.dispose?.();
      disposePage();
    },
  };
}
