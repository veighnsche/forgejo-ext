/**
 * @param {HTMLElement} root
 * @param {import('../../../../sdk/browser/extension-contract.js').MountContext} context
 * @returns {Promise<() => void>}
 */
export async function mount(root, context) {
  if (context.contractVersion !== 1) throw new TypeError('Unsupported browser contract version');
  const {request} = context;
  const description = document.createElement('p');
  description.textContent = 'This page and its backend were installed independently of the Forgejo executable.';
  const output = document.createElement('pre');
  output.dataset.extensionContext = '';
  output.textContent = 'Loading native request context…';
  root.append(description, output);
  const controller = new AbortController();
  try {
    const response = await request('context', {signal: controller.signal});
    if (!response.ok) throw new Error(`Request failed: ${response.status}`);
    output.textContent = JSON.stringify(await response.json(), null, 2);
  } catch (error) {
    if (!(error instanceof Error && error.name === 'AbortError')) {
      output.textContent = error instanceof Error ? error.message : 'Could not load context.';
    }
  }
  return () => controller.abort();
}
